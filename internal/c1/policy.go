package c1

const (
	MiB              int64 = 1 << 20
	KiB                    = 1 << 10
	MaxRegistryNodes       = 256
)

// Policy is the fixed runtime policy for the panel's measurement owner.
type Policy struct {
	Enabled bool
}

func DefaultPolicy() Policy {
	return Policy{Enabled: true}
}

func (p Policy) normalized() Policy {
	return p
}

type NodeState struct {
	ID      string
	Tag     string
	Enabled bool
	// SourceGroup is a private stable source identity used only for balanced
	// measurement. It must never contain a subscription URL or credential.
	SourceGroup string
}
