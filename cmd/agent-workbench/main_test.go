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

func TestConnectArguments(t *testing.T) {
	options, err := parseArgs([]string{"connect", "--agent-name", "评审", "--model", "custom-model", "--sandbox", "read-only", "--approval", "on-request", "--no-browser", "/tmp/project"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Action != "connect" || options.AgentName != "评审" || options.Model != "custom-model" || options.Sandbox != "read-only" || options.Approval != "on-request" {
		t.Fatalf("connect options lost: %+v", options)
	}
	for _, args := range [][]string{{"connect", "--sandbox", "danger-full-access", "/tmp/project"}, {"open", "--model", "x", "/tmp/project"}, {"connect", "--approval", "automatic", "/tmp/project"}} {
		if _, err := parseArgs(args); err == nil {
			t.Fatalf("invalid connect flags accepted: %v", args)
		}
	}
}
