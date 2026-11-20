// SPDX-License-Identifier: GPL-3.0-or-later

package ntfy

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMessageDecode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Message
	}{
		{
			name: "message with actions and attachment",
			in: `{"id":"sPs71M8A2T","time":1643935928,"event":"message","topic":"alerts",` +
				`"priority":5,"tags":["warning","skull"],"click":"https://example.org/incident/1234",` +
				`"attachment":{"name":"camera.jpg","type":"image/png","size":33848,"expires":1643946728,` +
				`"url":"https://ntfy.sh/file/sPs71M8A2T.png"},"title":"Unauthorized access detected",` +
				`"message":"Movement detected in the yard",` +
				`"actions":[{"action":"view","label":"Open portal","url":"https://home.example/","clear":true},` +
				`{"action":"http","label":"Turn down","url":"https://api.example/","method":"PUT",` +
				`"headers":{"X-Token":"t"},"body":"{\"temperature\": 65}"}]}`,
			want: Message{
				ID:       "sPs71M8A2T",
				Time:     1643935928,
				Event:    EventMessage,
				Topic:    "alerts",
				Message:  "Movement detected in the yard",
				Title:    "Unauthorized access detected",
				Priority: 5,
				Tags:     []string{"warning", "skull"},
				Click:    "https://example.org/incident/1234",
				Actions: []Action{
					{Action: "view", Label: "Open portal", URL: "https://home.example/", Clear: true},
					{
						Action:  "http",
						Label:   "Turn down",
						URL:     "https://api.example/",
						Method:  "PUT",
						Headers: map[string]string{"X-Token": "t"},
						Body:    `{"temperature": 65}`,
					},
				},
				Attachment: &Attachment{
					Name:    "camera.jpg",
					Type:    "image/png",
					Size:    33848,
					Expires: 1643946728,
					URL:     "https://ntfy.sh/file/sPs71M8A2T.png",
				},
			},
		},
		{
			name: "message with only the required fields",
			in:   `{"id":"wze9zgqK41","time":1638542110,"event":"message","topic":"alerts","message":"hi"}`,
			want: Message{ID: "wze9zgqK41", Time: 1638542110, Event: EventMessage, Topic: "alerts", Message: "hi"},
		},
		{
			name: "open event",
			in:   `{"id":"2pgIAaGrQ8","time":1638542215,"event":"open","topic":"phil_alerts"}`,
			want: Message{ID: "2pgIAaGrQ8", Time: 1638542215, Event: EventOpen, Topic: "phil_alerts"},
		},
		{
			name: "keepalive event",
			in:   `{"id":"371sevb0pD","time":1638542275,"event":"keepalive","topic":"phil_alerts"}`,
			want: Message{ID: "371sevb0pD", Time: 1638542275, Event: EventKeepalive, Topic: "phil_alerts"},
		},
		{
			name: "unknown fields are ignored",
			in:   `{"id":"a","time":1,"event":"message","topic":"t","sequence_id":"s","icon":"x","message":"m"}`,
			want: Message{ID: "a", Time: 1, Event: EventMessage, Topic: "t", Message: "m"},
		},
		{
			name: "empty actions",
			in:   `{"id":"a","time":1,"event":"message","topic":"t","actions":[],"tags":[]}`,
			want: Message{ID: "a", Time: 1, Event: EventMessage, Topic: "t", Actions: []Action{}, Tags: []string{}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got Message
			if err := json.Unmarshal([]byte(c.in), &got); err != nil {
				t.Fatalf("json.Unmarshal(%q) = error %v; want no error", c.in, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Message(%q) = %+v; want %+v", c.in, got, c.want)
			}
		})
	}
}

func TestMessageDecodeErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "broken json", in: `{"id":`},
		{name: "id is a number", in: `{"id":1,"time":1,"event":"message","topic":"t"}`},
		{name: "priority is a string", in: `{"id":"a","time":1,"event":"message","topic":"t","priority":"high"}`},
		{name: "actions is a string", in: `{"id":"a","time":1,"event":"message","topic":"t","actions":"view, Open, https://x"}`},
		{name: "empty line", in: ``},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got Message
			if err := json.Unmarshal([]byte(c.in), &got); err == nil {
				t.Errorf("json.Unmarshal(%q) = nil; want a decoding error", c.in)
			}
		})
	}
}

func TestIsMessage(t *testing.T) {
	cases := []struct {
		event string
		want  bool
	}{
		{event: EventMessage, want: true},
		{event: EventOpen},
		{event: EventKeepalive},
		{event: EventPollRequest},
		{event: ""},
	}

	for _, c := range cases {
		if got := (Message{Event: c.event}).IsMessage(); got != c.want {
			t.Errorf("Message{Event: %q}.IsMessage() = %t; want %t", c.event, got, c.want)
		}
	}
}

func FuzzMessageDecode(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"id":"a","time":1,"event":"message","topic":"t","message":"m"}`,
		`{"id":"a","time":1,"event":"open","topic":"t"}`,
		`{"id":"a","actions":[{"action":"view","label":"l","url":"u"}]}`,
		`{"attachment":{"name":"n","url":"u","size":1}}`,
		`null`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		var m Message
		if err := json.Unmarshal([]byte(in), &m); err != nil {
			return
		}
		// A decoded message must round-trip without changing what the caller reads.
		out, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("json.Marshal(Message(%q)) = error %v; want no error", in, err)
		}
		var again Message
		if err := json.Unmarshal(out, &again); err != nil {
			t.Fatalf("json.Unmarshal(json.Marshal(Message(%q))) = error %v", in, err)
		}
		if !reflect.DeepEqual(m, again) {
			t.Fatalf("Message(%q) = %+v after a round trip; want %+v", in, again, m)
		}
	})
}
