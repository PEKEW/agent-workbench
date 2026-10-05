package main

import "testing"

func TestArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want options
	}{{nil, options{}}, {[]string{"open", "--no-browser", "/tmp/项目"}, options{Action: "open", Path: "/tmp/项目", NoBrowser: true}}, {[]string{"new", "--name", "demo", "--port", "8123", "/tmp/new"}, options{Action: "new", Name: "demo", Path: "/tmp/new", Port: 8123}}, {[]string{"--no-browser"}, options{NoBrowser: true}}} {
		got, err := parseArgs(tc.args)
		if err != nil || got != tc.want {
			t.Fatalf("%v: %+v %v", tc.args, got, err)
		}
	}
}
func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"delete"}, {"open"}, {"new", "/tmp/new"}, {"open", "a", "b"}, {"--port", "-1"}, {"--port", "65536"}, {"--name", "ignored"}, {"open", "a", "--no-browser"}} {
		if _, err := parseArgs(args); err == nil {
			t.Fatalf("接受无效参数 %v", args)
		}
	}
}
