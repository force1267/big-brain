package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// durableConfig is the per-flow durability configuration set by Durable(opts…).
// Only the structure-version guard (forwardCompatible) is wired at the flow
// layer; the resume trigger, retries, and ttl govern how a durable flow is
// scheduled/retried and are honoured once triggers wire in the engine — they are
// recorded here so the API is stable.
type durableConfig struct {
	forwardCompatible bool          // resume even if the flow's structure changed
	resumeOnReReg     bool          // resume on re-registration rather than at startup
	retries           int           // 0 = engine default
	ttl               time.Duration // 0 = inherit/none
}

// DurableOpt configures a Durable flow.
type DurableOpt func(*durableConfig)

func newDurableConfig(opts []DurableOpt) durableConfig {
	var c durableConfig
	for _, o := range opts {
		o(&c)
	}
	return c
}

// ForwardCompatible opts out of the structure-version guard: the flow resumes
// from its checkpoint even if its graph changed since. The author asserts the
// change is compatible.
func ForwardCompatible() DurableOpt { return func(c *durableConfig) { c.forwardCompatible = true } }

// ResumeOnReregister resumes a crashed durable run only when its id is
// registered again (for flows that exist dynamically, not at startup), rather
// than automatically at startup.
func ResumeOnReregister() DurableOpt { return func(c *durableConfig) { c.resumeOnReReg = true } }

// Retries sets how many times the engine retries the durable flow on failure.
func Retries(n int) DurableOpt { return func(c *durableConfig) { c.retries = n } }

// TTL bounds how long a pending durable run is kept before it is dropped.
func TTL(d time.Duration) DurableOpt { return func(c *durableConfig) { c.ttl = d } }

// structureSig is a stable signature of a flow's shape, so a durable flow can
// tell whether its graph changed between runs (see checkpoint.versionChanged).
func structureSig(f Flow) string {
	var b strings.Builder
	writeSig(&b, f)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}

func writeSig(b *strings.Builder, f Flow) {
	switch v := f.(type) {
	case *decorated:
		b.WriteString("dec(")
		b.WriteString(v.fid)
		b.WriteByte(':')
		writeSig(b, v.inner)
		b.WriteByte(')')
	case *Basic:
		b.WriteString("basic(" + v.fid + ":" + strconv.Itoa(len(v.agents)) + ")")
	case seq:
		b.WriteString("seq[")
		for _, s := range v.steps {
			writeSig(b, s)
			b.WriteByte(',')
		}
		b.WriteByte(']')
	case *selectGroup:
		writeMembersSig(b, "sel", v.members)
	case allGroup:
		writeMembersSig(b, "all", v.members)
	case oneGroup:
		writeMembersSig(b, "one", v.members)
	case groupGroup:
		writeMembersSig(b, "grp", v.members)
	case respond:
		b.WriteString("respond")
	case notify:
		b.WriteString("notify")
	}
}

func writeMembersSig(b *strings.Builder, kind string, members []Flow) {
	b.WriteString(kind + "[")
	for _, m := range members {
		writeSig(b, m)
		b.WriteByte(',')
	}
	b.WriteByte(']')
}
