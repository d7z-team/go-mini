package runtime

import "testing"

func cleanupTestInstance(t *testing.T, instance *Instance) {
	t.Helper()
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close instance: %v", err)
		}
	})
}
