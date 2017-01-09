package googlephotos

import "testing"

func TestBestDownloadURL(t *testing.T) {
	fb := &EntryContent{URL: "fallback"}

	for i, test := range []struct {
		input  Entry
		expect string
	}{
		{
			input: Entry{Content: fb, Media: &EntryMedia{Content: []MediaContent{
				{URL: "u1.jpg", Type: "image/jpeg", Width: 1, Height: 1, Medium: "image"},
				{URL: "u2.jpg", Type: "image/jpeg", Width: 2, Height: 2, Medium: "image"},
				{URL: "u3.jpg", Type: "image/jpeg", Width: 3, Height: 3, Medium: "image"},
			}}},
			expect: "u3.jpg",
		},
		{
			input: Entry{Content: fb, Media: &EntryMedia{Content: []MediaContent{
				{URL: "u3.jpg", Type: "image/jpeg", Width: 3, Height: 3, Medium: "image"},
				{URL: "u2.jpg", Type: "image/jpeg", Width: 2, Height: 2, Medium: "image"},
				{URL: "u1.jpg", Type: "image/jpeg", Width: 1, Height: 1, Medium: "image"},
			}}},
			expect: "u3.jpg",
		},
		{
			input: Entry{Content: fb, Media: &EntryMedia{Content: []MediaContent{
				{URL: "u1.flv", Type: "application/x-shockwave-flash", Width: 3, Height: 3, Medium: "video"},
				{URL: "u2.mp4", Type: "video/mpeg4", Width: 2, Height: 2, Medium: "video"},
				{URL: "u3.jpg", Type: "image/gif", Width: 1, Height: 1, Medium: "image"},
			}}},
			expect: "u2.mp4", // prefer non-flash formats, even if lower-res
		},
		{
			input: Entry{Content: fb, Media: &EntryMedia{Content: []MediaContent{
				{URL: "u1.mp4", Type: "video/mpeg4", Width: 2, Height: 2, Medium: "video"},
				{URL: "u2.mp4", Type: "video/mpeg4", Width: 1, Height: 1, Medium: "video"},
			}}},
			expect: "u1.mp4",
		},
		{
			input:  Entry{Content: fb},
			expect: "fallback",
		},
		{
			input:  Entry{},
			expect: "",
		},
	} {
		actual, err := getBestDownloadURL(test.input)
		if actual != test.expect {
			t.Errorf("Test %d: Got '%s', expected '%s'", i, actual, test.expect)
		}
		if test.expect != "" && err != nil {
			t.Errorf("Test %d: Did not expect an error, got '%v'", i, err)
		}
		if test.expect == "" && err == nil {
			t.Errorf("Test %d: Expected an error, didn't get one", i)
		}
	}
}
