#!/bin/sh
set -eu

failures=0

# Fleet uses this reserved identity for synthetic workspace commits and fixtures.
synthetic_fleet_email="fleet@example.invalid"

report_files() {
	label=$1
	files=$2
	if [ -z "$files" ]; then
		return
	fi

	echo "$label" >&2
	printf '%s\n' "$files" | sed 's/^/  - /' >&2
	failures=$((failures + 1))
}

restricted_paths() {
	while IFS= read -r path; do
		[ -n "$path" ] || continue
		case "$path" in
			.env|*/.env|.env.*|*/.env.*)
				case "$path" in
					*.example) ;;
					*) printf '%s\n' "$path" ;;
				esac
				;;
			deploy/secrets/*|deploy/backups/*|deploy/nginx/*|deploy/script/*)
				printf '%s\n' "$path"
				;;
			*.pem|*.key|*.p12|*.pfx|*.dump|*.bak|*.log)
				printf '%s\n' "$path"
				;;
		esac
	done | sort -u
}

tracked_path_findings=$(git ls-files | restricted_paths)
report_files "Restricted paths are tracked:" "$tracked_path_findings"

history_path_findings=$(
	git log --all --name-only --format= |
		restricted_paths
)
report_files "Restricted paths remain in reachable Git history:" "$history_path_findings"

email_findings=$(
	git grep -nI -E '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' -- . 2>/dev/null |
		awk -F: -v synthetic_fleet_email="$synthetic_fleet_email" '
			{
				file = $1
				text = $0
				sub(/^[^:]*:[0-9]+:/, "", text)
				while (match(text, /[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/)) {
					email = substr(text, RSTART, RLENGTH)
					if (email != synthetic_fleet_email && email !~ /@(example\.com|example\.test|users\.noreply\.github\.com)$/) {
						print file
					}
					text = substr(text, RSTART + RLENGTH)
				}
			}
		' |
		sort -u
)
report_files "Non-example email addresses are present in tracked content:" "$email_findings"

private_network_findings=$(
	git grep -lI -E '\b(10\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}|192\.168\.[0-9]{1,3}\.[0-9]{1,3}|172\.(1[6-9]|2[0-9]|3[01])\.[0-9]{1,3}\.[0-9]{1,3})\b' -- . 2>/dev/null |
		sort -u || true
)
report_files "Private network addresses are present in tracked content:" "$private_network_findings"

path_separator=/
local_path_pattern="(${path_separator}Users${path_separator}|${path_separator}home${path_separator}[A-Za-z0-9._-]+${path_separator}|${path_separator}srv${path_separator}[A-Za-z0-9._-]+)"
local_path_findings=$(
	git grep -lI -E "$local_path_pattern" -- . 2>/dev/null |
		sort -u || true
)
report_files "Machine-specific or deployment paths are present in tracked content:" "$local_path_findings"

# Pull request workflows and update-branch operations create GitHub-authored
# merge commits. Their committer and two-parent shape identify the synthetic
# merge even when GitHub supplies the account email as author metadata. Branch
# commits must still use a GitHub noreply or the reserved Fleet identity.
at_sign=@
github_committer_email="noreply${at_sign}github.com"
author_findings=$(
	git log --all --format='%ae%x09%ce%x09%P%x09%s' |
		sort -u |
		awk -F '	' \
			-v github_committer_email="$github_committer_email" \
			-v synthetic_fleet_email="$synthetic_fleet_email" '
			$1 == synthetic_fleet_email { next }
			$1 ~ /@users\.noreply\.github\.com$/ { next }
			$2 == github_committer_email && split($3, parents, " ") == 2 { next }
			$2 == github_committer_email &&
				$4 ~ /^Merge [0-9a-f]+ into [0-9a-f]+$/ { next }
			$2 == github_committer_email &&
				$4 ~ /^Merge pull request #[0-9]+ from [A-Za-z0-9_.-]+\/[A-Za-z0-9_.\/-]+$/ { next }
			$2 == github_committer_email &&
				$4 ~ / \(#[0-9]+\)$/ { next }
			{ print "non-public-author-email" }
		' |
		sort -u
)
if [ -n "$author_findings" ]; then
	echo "Reachable commits contain an author email that is not a GitHub noreply address." >&2
	failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
	echo "Public repository policy check failed." >&2
	exit 1
fi

echo "Public repository policy check passed."
