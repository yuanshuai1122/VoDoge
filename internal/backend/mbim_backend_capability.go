package backend

import "github.com/yuanshuai1122/vodoge/pkg/mbim"

func (b *MBIMBackend) Capability() *mbim.Capabilities {
	return b.source.Capability()
}
