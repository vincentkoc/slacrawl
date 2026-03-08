package search

import (
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

func TestNormalizeMessage(t *testing.T) {
	msg := slack.Message{}
	msg.Text = "Hello <@U123|alice> in <#C123|eng> <https://example.com|docs>"
	msg.Files = []slack.File{{Title: "runbook", Name: "runbook.md"}}
	msg.Edited = &slack.Edited{Timestamp: "123.45"}

	normalized := NormalizeMessage(msg)
	require.Contains(t, normalized, "@alice")
	require.Contains(t, normalized, "#eng")
	require.Contains(t, normalized, "docs https://example.com")
	require.Contains(t, normalized, "runbook")
	require.Contains(t, normalized, "[edited]")
}

func TestExtractMentions(t *testing.T) {
	mentions := ExtractMentions("hello <@U123|alice> and <#C123|eng>")
	require.Len(t, mentions, 2)
	require.Equal(t, "user", mentions[0].Type)
	require.Equal(t, "U123", mentions[0].TargetID)
	require.Equal(t, "channel", mentions[1].Type)
}
