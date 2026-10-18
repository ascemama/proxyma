package policy

import "testing"

func TestCompileTemplateTypedPlaceholders(t *testing.T) {
	matcher, err := CompileTemplate("ping {ipv4}")
	if err != nil {
		t.Fatal(err)
	}
	if !matcher.Match("ping 1.1.1.1") || matcher.Match("ping 999.1.1.1") {
		t.Fatal("ipv4 template matching is incorrect")
	}
}

func TestCompileTemplateRejectsUnsafeOrAmbiguousTemplates(t *testing.T) {
	for _, template := range []string{"", "echo {unknown}", "echo {ipv4}/x", "echo ok && reboot", "rm {path}"} {
		if _, err := CompileTemplate(template); err == nil && template != "rm {path}" {
			t.Fatalf("template %q was accepted", template)
		}
	}
}

func TestCompileTemplateAllowsTypedPath(t *testing.T) {
	matcher, err := CompileTemplate("rm -rf {path}")
	if err != nil {
		t.Fatal(err)
	}
	if !matcher.Match("rm -rf /tmp/demo") || matcher.Match("rm -rf /tmp/demo;whoami") {
		t.Fatal("path template did not match")
	}
}
