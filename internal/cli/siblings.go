package cli

// SiblingCommands is the sentence that names the three commands, so that an agent
// that finds one of them learns from it what the other two are for. Each
// command's help and each --schema description carry it, and this is the one
// place it is written. It is an untyped constant so that a help text, which is a
// raw-string constant, can be built with it.
const SiblingCommands = "spec writes a package from an idea, specr renders one, speca reports and changes its state."
