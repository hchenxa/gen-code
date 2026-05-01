package tool

import "testing"

func TestTaskOutputIsNotInDefaultToolSet(t *testing.T) {
	set := &Set{}
	for _, schema := range set.Tools() {
		if schema.Name == ToolTaskOutput {
			t.Fatalf("did not expect disabled tool %s in tool set", ToolTaskOutput)
		}
	}
}
