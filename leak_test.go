package esodm

import (
	"go.uber.org/goleak"
	"testing"
)

// Verify the whole suite, including cancellation and early callback failures.
func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }
