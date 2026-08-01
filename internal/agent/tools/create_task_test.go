package tools

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCreateTaskInputTreatsEmptyOptionalIDArraysAsNull(t *testing.T) {
	var input CreateTaskInput
	err := json.Unmarshal([]byte(`{
		"title":"Investigate cache refresh",
		"context":"A refresh failed after release.",
		"expected_result":"The cause and repair direction are documented.",
		"project_number":12,
		"milestone_id":[],
		"assignee_id":[],
		"due_date":null,
		"priority":"none"
	}`), &input)

	require.NoError(t, err)
	require.Nil(t, input.MilestoneID)
	require.Nil(t, input.AssigneeID)

	err = json.Unmarshal([]byte(`{
		"milestone_id":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16]
	}`), &input)
	require.NoError(t, err)
	require.NotNil(t, input.MilestoneID)
	require.Equal(t, "01020304-0506-0708-090a-0b0c0d0e0f10", *input.MilestoneID)

	err = json.Unmarshal([]byte(`{"milestone_id":["not-a-uuid"]}`), &input)
	require.Error(t, err)
}

func TestParseOptionalDueDateAcceptsJSONNullStringFromModel(t *testing.T) {
	for _, value := range []string{"", "null", " NULL "} {
		parsed, present, err := parseOptionalDueDate(value)
		require.NoError(t, err)
		require.False(t, present)
		require.True(t, parsed.IsZero())
	}

	parsed, present, err := parseOptionalDueDate("2026-08-01")
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), parsed)

	_, _, err = parseOptionalDueDate("tomorrow")
	require.Error(t, err)
}

func TestParseOptionalUUIDAcceptsNullStringFromModel(t *testing.T) {
	for _, value := range []string{"", "null", " NULL "} {
		parsed, present, err := parseOptionalUUID(value)
		require.NoError(t, err)
		require.False(t, present)
		require.Equal(t, uuid.Nil, parsed)
	}

	want := uuid.New()
	parsed, present, err := parseOptionalUUID(want.String())
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, want, parsed)

	_, _, err = parseOptionalUUID("not-a-uuid")
	require.Error(t, err)
}
