Subscribe to several ntfy topics at once by writing them comma separated in `topic` (for example
`topic = "casa, pruebas"`). One connection delivers them all, each message keeps its own topic, and
`publish` asks which one to use when the configuration names more than one.
