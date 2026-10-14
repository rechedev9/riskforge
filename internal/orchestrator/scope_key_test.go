package orchestrator

import "testing"

func TestScopeKeyEncode_DoesNotCollideOnColon(t *testing.T) {
	t.Parallel()
	pairs := [][2]scopeKey{
		{
			{clientID: "ab", requestID: "c:d"},
			{clientID: "ab:c", requestID: "d"},
		},
		{
			{clientID: "", requestID: "ab:c"},
			{clientID: "ab", requestID: "c"},
		},
		{
			{clientID: "a:b", requestID: "c"},
			{clientID: "a", requestID: "b:c"},
		},
	}
	for _, pair := range pairs {
		a := pair[0].encode()
		b := pair[1].encode()
		if a == b {
			t.Fatalf("keys collided: %+v and %+v both %q", pair[0], pair[1], a)
		}
	}
}
