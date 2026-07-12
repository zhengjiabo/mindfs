package api

import (
	"strings"
	"testing"

	"mindfs/server/internal/kanban"
)

func TestTaskSessionTitleSourcePrefersLastTaskMarker(t *testing.T) {
	prompt := strings.Join([]string{
		"背景：这是固定流程，其中可能提到任务：但不是最终输入。",
		"过程：先写需求，再开发。",
		"任务：补充页面分享功能",
		"",
		"Task control context:",
		"- task_number: 15",
	}, "\n")

	if got := taskSessionTitleSource(prompt); got != "补充页面分享功能" {
		t.Fatalf("taskSessionTitleSource() = %q", got)
	}
}

func TestTaskSessionTitleSourceSupportsAsciiColon(t *testing.T) {
	if got := taskSessionTitleSource("固定说明\n任务: 检查英雄评分跳转方案"); got != "检查英雄评分跳转方案" {
		t.Fatalf("taskSessionTitleSource() = %q", got)
	}
}

func TestTaskSessionTitleSourceIgnoresEmbeddedTaskMarker(t *testing.T) {
	prompt := "任务：设计批量处理流程\n其中包含子任务：检查分享页面"
	if got := taskSessionTitleSource(prompt); got != "设计批量处理流程\n其中包含子任务：检查分享页面" {
		t.Fatalf("taskSessionTitleSource() = %q", got)
	}
}

func TestTaskSessionTitleSourceFallsBackToPrompt(t *testing.T) {
	if got := taskSessionTitleSource("  安装 image-to-code 技能  "); got != "安装 image-to-code 技能" {
		t.Fatalf("taskSessionTitleSource() = %q", got)
	}
}

func TestTaskSessionTitleSourceFallsBackWhenMarkerHasNoContent(t *testing.T) {
	if got := taskSessionTitleSource("检查配置\n任务："); got != "检查配置\n任务：" {
		t.Fatalf("taskSessionTitleSource() = %q", got)
	}
}

func TestTaskSessionInitialNameIncludesNumberAndSummary(t *testing.T) {
	name, source, prefix := taskSessionInitialName(kanban.Task{TaskNumber: 14, TaskTemplateName: "流程"}, "背景\n任务：安装 image-to-code 技能")
	if name != "#14 · 安装 image-to-code 技能" {
		t.Fatalf("name = %q", name)
	}
	if source != "安装 image-to-code 技能" {
		t.Fatalf("source = %q", source)
	}
	if prefix != "#14 · " {
		t.Fatalf("prefix = %q", prefix)
	}
}

func TestTaskSessionInitialNameFallsBackToLegacyName(t *testing.T) {
	name, source, prefix := taskSessionInitialName(kanban.Task{TaskNumber: 16, TaskTemplateName: "流程"}, "")
	if name != "流程 / #16" || source != "" || prefix != "#16 · " {
		t.Fatalf("name/source/prefix = %q/%q/%q", name, source, prefix)
	}
}
