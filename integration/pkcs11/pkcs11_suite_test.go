/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pkcs11

import (
	"encoding/json"
	"testing"

	bpkcs11 "github.com/hyperledger/fabric/bccsp/pkcs11"
	"github.com/hyperledger/fabric/integration"
	"github.com/hyperledger/fabric/integration/nwo"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestPKCS11(t *testing.T) {
	RegisterFailHandler(Fail)
	lib, pin, label := bpkcs11.FindPKCS11Lib()
	if lib == "" || pin == "" || label == "" {
		t.Skip("Skipping PKCS11 Suite: Required ENV variables not set")
	}
	RunSpecs(t, "PKCS11 Suite")
}

var (
	buildServer *nwo.BuildServer
	components  *nwo.Components
)

var _ = SynchronizedBeforeSuite(func() []byte {
	buildServer = nwo.NewBuildServer("-tags=pkcs11")
	buildServer.Serve()

	components = buildServer.Components()
	payload, err := json.Marshal(components)
	Expect(err).NotTo(HaveOccurred())
	return payload
}, func(payload []byte) {
	err := json.Unmarshal(payload, &components)
	Expect(err).NotTo(HaveOccurred())
})

var _ = SynchronizedAfterSuite(func() {
}, func() {
	buildServer.Shutdown()
})

func StartPort() int {
	return integration.PKCS11Port.StartPortForNode()
}
