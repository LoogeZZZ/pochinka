package paths

import "testing"

func TestEmptyDACLNotNil(t *testing.T) {
	d, err := emptyDACL()
	if err != nil || d == nil {
		t.Fatalf("emptyDACL: %v %v", d, err)
	}
}
