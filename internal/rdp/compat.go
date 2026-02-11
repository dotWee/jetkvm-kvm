package rdp

import "github.com/kdsmith18542/gordp"

// protocolOptionSentinel keeps the protocol dependency explicit while all RDP
// protocol interaction remains isolated in this package.
var protocolOptionSentinel = gordp.Option{}
