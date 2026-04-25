package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootSkillDocumentIsSymlink(t *testing.T) {
	rootSkill := filepath.Join("..", "..", "SKILL.md")

	info, err := os.Lstat(rootSkill)
	if err != nil {
		t.Fatalf("stat root SKILL.md: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("root SKILL.md must be a symlink to internal/skill/SKILL.md")
	}

	target, err := os.Readlink(rootSkill)
	if err != nil {
		t.Fatalf("read root SKILL.md symlink: %v", err)
	}
	if target != filepath.Join("internal", "skill", "SKILL.md") {
		t.Fatalf("root SKILL.md points to %q, want %q", target, filepath.Join("internal", "skill", "SKILL.md"))
	}
}
