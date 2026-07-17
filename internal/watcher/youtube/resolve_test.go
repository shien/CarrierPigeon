package youtube

import "testing"

func TestParseChannelInput(t *testing.T) {
	cases := []struct {
		in      string
		kind    ChannelRefKind
		value   string
		wantErr bool
	}{
		{in: "UC1234567890abcdefghij_-", kind: ChannelRefID, value: "UC1234567890abcdefghij_-"},
		{in: "  UC1234567890abcdefghij_-  ", kind: ChannelRefID, value: "UC1234567890abcdefghij_-"},
		{in: "@somehandle", kind: ChannelRefHandle, value: "@somehandle"},
		{in: "https://www.youtube.com/channel/UC1234567890abcdefghij_-", kind: ChannelRefID, value: "UC1234567890abcdefghij_-"},
		{in: "https://www.youtube.com/channel/UC1234567890abcdefghij_-/videos", kind: ChannelRefID, value: "UC1234567890abcdefghij_-"},
		{in: "https://www.youtube.com/@somehandle", kind: ChannelRefHandle, value: "@somehandle"},
		{in: "https://www.youtube.com/@somehandle/streams", kind: ChannelRefHandle, value: "@somehandle"},
		{in: "https://m.youtube.com/@somehandle", kind: ChannelRefHandle, value: "@somehandle"},
		{in: "http://youtube.com/@somehandle?si=xyz", kind: ChannelRefHandle, value: "@somehandle"},
		{in: "https://www.youtube.com/user/legacyname", kind: ChannelRefUsername, value: "legacyname"},
		{in: "not a channel", wantErr: true},
		{in: "https://www.youtube.com/watch?v=abc", wantErr: true},
		{in: "https://example.com/@somehandle", wantErr: true},
		{in: "UCtooshort", wantErr: true},
	}
	for _, tc := range cases {
		got, err := ParseChannelInput(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseChannelInput(%q): want error, got %+v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseChannelInput(%q): %v", tc.in, err)
			continue
		}
		if got.Kind != tc.kind || got.Value != tc.value {
			t.Errorf("ParseChannelInput(%q) = {%v %q}, want {%v %q}", tc.in, got.Kind, got.Value, tc.kind, tc.value)
		}
	}
}
