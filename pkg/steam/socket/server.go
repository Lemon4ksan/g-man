// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

// CMServer specifies host endpoint, protocol type, and load metrics for a Steam Connection Manager.
type CMServer struct {
	Endpoint string
	Type     string
	Load     float64
	Realm    string
}
