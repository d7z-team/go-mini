package runtimes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTime(t *testing.T) {
	t.Run("now_and_format", func(t *testing.T) {
		result := runTest(t, `
			import "time"
			func main() {
				now := time.Now()
				push(now.Format("2006"))
			}
		`)
		assert.Len(t, result, 1)
		assert.Equal(t, "2026", result[0])
	})

	t.Run("parse_and_unix", func(t *testing.T) {
		result := runTest(t, `
			import "time"
			func main() {
				t, _ := time.Parse("2006-01-02", "2025-01-01")
				push(t.Year())
				push(t.Month())
				push(t.Day())
				push(t.Unix() > 0)
			}
		`)
		assert.Equal(t, []string{"2025", "1", "1", "true"}, result)
	})
}
