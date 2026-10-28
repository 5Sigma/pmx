package compose

import "testing"

func TestStripReplyPrefixes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Re: Hello", "Hello"},
		{"Re: Re: Hello", "Hello"},
		{"re: hello", "hello"},
		{"AW: Re: Topic", "Topic"},
		{"Hello", "Hello"},
		{"RE: RE: RE: Deep", "Deep"},
		{"SV: Meeting notes", "Meeting notes"},
		{"r: quick", "quick"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := StripReplyPrefixes(tc.input)
			if got != tc.want {
				t.Errorf("StripReplyPrefixes(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestStripAllPrefixes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Re: Fwd: Re: Topic", "Topic"},
		{"Fwd: Hello", "Hello"},
		{"fwd: re: fw: Mixed", "Mixed"},
		{"FW: Subject", "Subject"},
		{"Already clean", "Already clean"},
		{"Re: Hello", "Hello"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := StripAllPrefixes(tc.input)
			if got != tc.want {
				t.Errorf("StripAllPrefixes(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
