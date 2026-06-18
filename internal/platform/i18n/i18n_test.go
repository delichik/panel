package i18n

import (
	"strings"
	"testing"
)

func TestTranslateLocalePreservesRuntimeErrorDetails(t *testing.T) {
	got := TranslateLocale(LocaleSimplifiedChinese, "application_runtime_operation_failed", "Application runtime operation failed: create container failed")
	if !strings.HasPrefix(got, "应用运行时操作失败：") || !strings.Contains(got, "create container failed") {
		t.Fatalf("translation = %q", got)
	}
}

func TestTranslateLocaleUsesSpecificRuntimeDeploymentPrefix(t *testing.T) {
	got := TranslateLocale(LocaleSimplifiedChinese, "application_runtime_operation_failed", "Application runtime operation failed: deployment failed on 1 of 2 targets: srv-a: agent down")
	if !strings.HasPrefix(got, "应用运行时操作失败：部署失败目标 1 of 2 targets") || !strings.Contains(got, "srv-a: agent down") {
		t.Fatalf("translation = %q", got)
	}
}
