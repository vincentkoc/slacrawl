package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeValueHandlesTimePointers(t *testing.T) {
	now := time.Date(2026, time.April, 22, 3, 4, 5, 0, time.UTC)
	var nilTime *time.Time

	value := normalizeValue(struct {
		At      *time.Time `json:"at"`
		Missing *time.Time `json:"missing"`
	}{
		At:      &now,
		Missing: nilTime,
	})

	row := value.(map[string]any)
	require.Equal(t, now, row["at"])
	require.Nil(t, row["missing"])
}
