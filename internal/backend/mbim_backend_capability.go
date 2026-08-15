package backend

import "github.com/yuanshuai1122/vodog/pkg/mbim"

func (b *MBIMBackend) Capability() *mbim.Capabilities {
	return b.source.Capability()
}
