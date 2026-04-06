package skill

import _ "embed"

//go:generate cp ../../SKILL.md .

//go:embed SKILL.md
var Content string
