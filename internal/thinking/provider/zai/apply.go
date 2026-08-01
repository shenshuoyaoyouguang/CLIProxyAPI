// Package zai implements thinking configuration for Z.ai GLM models.
//
// Z.ai models use OpenAI-compatible reasoning format with effort levels.
package zai

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/openai"
)

// Applier implements thinking.ProviderApplier for Z.ai models.
type Applier struct {
	openai.Applier
}

var _ thinking.ProviderApplier = (*Applier)(nil)

// NewApplier creates a new Z.ai thinking applier.
func NewApplier() *Applier {
	return &Applier{}
}

func init() {
	thinking.RegisterProvider("zai", NewApplier())
}
