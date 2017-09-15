// constants.go - Katzenpost client constants.
// Copyright (C) 2017  Yawning Angel.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

// Package constants contains the client constants for Katzenpost.
package constants

import (
	"time"
)

const (
	// DatabaseConnectTimeout is a duration used as the connect timeout
	// when we access our local databases (for POP3&SMTP proxies)
	DatabaseConnectTimeout = 3 * time.Second

	// PoissonLambda is the poisson distribution's lambda parameter
	// used for setting mix hop delays
	// XXX fix me
	PoissonLambda = float64(.234)

	// HopsPerPath is the number of mix hops per path through the mix network
	HopsPerPath = 3

	// MessageIDLength is the length of a message ID in bytes.
	MessageIDLength = 16

	// EndToEndKeyType is the string representing the end to end
	// messaging key type
	EndToEndKeyType = "e2e"

	// LinkLayerKeyType is the string representing the link layer
	// wire protocol key type
	LinkLayerKeyType = "wire"

	// DefaultSMTPNetwork is the default network type used for our SMTP proxy service
	DefaultSMTPNetwork = "tcp"

	// DefaultSMTPAddress is the default address used for our SMTP proxy service
	DefaultSMTPAddress = "127.0.0.1:2525"

	// DefaultPOP3Network is the default network type used for our POP3 proxy service
	DefaultPOP3Network = "tcp"

	// DefaultPOP3Address is the default address type used for our POP3 proxy service
	DefaultPOP3Address = "127.0.0.1:1110"
)
