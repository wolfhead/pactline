import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AdminConnectionsPage from "./AdminConnectionsPage";
import { createRepositoryConnection, listRepositoryConnections } from "@/api/admin-repository-connections";

vi.mock("@/api/admin-repository-connections", () => ({
  createRepositoryConnection: vi.fn(),
  disableRepositoryConnection: vi.fn(),
  listRepositoryConnections: vi.fn(),
  rotateRepositoryCredential: vi.fn(),
  validateRepositoryConnection: vi.fn(),
}));

const CONNECTION = {
  id: "connection-1",
  version: 1,
  label: "App",
  origin: "https://gitlab.example",
  provider: "gitlab" as const,
  provider_repository_id: "42",
  path_with_namespace: "team/app",
  canonical_web_url: "https://gitlab.example/team/app",
  default_branch: "main",
  credential_expires_at: null,
  status: "active" as const,
  last_validated_at: "2026-08-13T08:00:00Z",
  created_at: "2026-08-13T08:00:00Z",
  updated_at: "2026-08-13T08:00:00Z",
};

describe("AdminConnectionsPage", () => {
  afterEach(cleanup);
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(listRepositoryConnections).mockResolvedValue([]);
    vi.mocked(createRepositoryConnection).mockResolvedValue(CONNECTION);
  });

  it("clears the write-only token as soon as creation starts", async () => {
    let resolveCreate: ((value: typeof CONNECTION) => void) | undefined;
    vi.mocked(createRepositoryConnection).mockReturnValue(
      new Promise((resolve) => {
        resolveCreate = resolve;
      }),
    );
    render(<AdminConnectionsPage />);
    await screen.findByText("尚未配置 Repository Connection。");
    fireEvent.change(screen.getByLabelText("显示名称"), {
      target: { value: "App" },
    });
    fireEvent.change(screen.getByLabelText("仓库地址"), {
      target: { value: CONNECTION.canonical_web_url },
    });
    const token = screen.getByLabelText(/只读 Access Token/) as HTMLInputElement;
    fireEvent.change(token, { target: { value: "secret-token" } });
    fireEvent.click(screen.getByRole("button", { name: "创建并鉴权" }));
    expect(token.value).toBe("");
    expect(createRepositoryConnection).toHaveBeenCalledWith({
      label: "App",
      repository_url: CONNECTION.canonical_web_url,
      credential: "secret-token",
      credential_expires_at: null,
      provider: "gitlab",
    });
    resolveCreate?.(CONNECTION);
    await waitFor(() => expect(screen.getByText(/已创建 team\/app/)).toBeInTheDocument());
  });

  it("does not present a loading failure as an empty Connection list", async () => {
    vi.mocked(listRepositoryConnections).mockRejectedValue(new Error("provider unavailable"));
    render(<AdminConnectionsPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent("provider unavailable");
    expect(screen.queryByText("尚未配置 Repository Connection。")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重试加载" })).toBeInTheDocument();
  });

  it("creates a GitHub Connection with an explicit provider", async () => {
    const githubConnection = {
      ...CONNECTION,
      provider: "github" as const,
      origin: "https://github.com",
      canonical_web_url: "https://github.com/team/app",
    };
    vi.mocked(createRepositoryConnection).mockResolvedValue(githubConnection);
    render(<AdminConnectionsPage />);
    await screen.findByText("尚未配置 Repository Connection。");
    fireEvent.change(screen.getByLabelText("Provider"), { target: { value: "github" } });
    fireEvent.change(screen.getByLabelText("显示名称"), { target: { value: "GitHub App" } });
    fireEvent.change(screen.getByLabelText("仓库地址"), { target: { value: githubConnection.canonical_web_url } });
    fireEvent.change(screen.getByLabelText(/只读 Access Token/), { target: { value: "github-token" } });
    fireEvent.click(screen.getByRole("button", { name: "创建并鉴权" }));
    await waitFor(() => expect(createRepositoryConnection).toHaveBeenCalledWith({
      label: "GitHub App",
      provider: "github",
      repository_url: githubConnection.canonical_web_url,
      credential: "github-token",
      credential_expires_at: null,
    }));
  });
});
