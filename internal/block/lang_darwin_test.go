package block

import "testing"

func TestLangFor(t *testing.T) {
	have := map[string]bool{"pt_PT.UTF-8": true, "en_US.UTF-8": true}
	exists := func(l string) bool { return have[l] }
	for in, want := range map[string]string{
		"pt_PT":          "pt_PT.UTF-8",
		"pt_PT@rg=ptzzz": "pt_PT.UTF-8",
		"en_PT":          "en_US.UTF-8", // not a system locale
		"":               "en_US.UTF-8",
	} {
		if got := langFor(in, exists); got != want {
			t.Errorf("langFor(%q) = %q, want %q", in, got, want)
		}
	}
}
