/*
Copyright IBM Corp All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package gateway

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/gateway"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/integration/nwo"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ = Describe("GatewayService", func() {
	var (
		testDir         string
		network         *nwo.Network
		org1Peer0       *nwo.Peer
		process         ifrit.Process
		conn            *grpc.ClientConn
		gatewayClient   gateway.GatewayClient
		ctx             context.Context
		cancel          context.CancelFunc
		signingIdentity *nwo.SigningIdentity
	)

	BeforeEach(func() {
		var err error
		testDir, err = ioutil.TempDir("", "gateway")
		Expect(err).NotTo(HaveOccurred())

		client, err := docker.NewClientFromEnv()
		Expect(err).NotTo(HaveOccurred())

		config := nwo.BasicEtcdRaft()
		network = nwo.New(config, testDir, client, StartPort(), components)

		network.GatewayEnabled = true

		network.GenerateConfigTree()
		network.Bootstrap()

		networkRunner := network.NetworkGroupRunner()
		process = ifrit.Invoke(networkRunner)
		Eventually(process.Ready(), network.EventuallyTimeout).Should(BeClosed())

		orderer := network.Orderer("orderer")
		network.CreateAndJoinChannel(orderer, "testchannel")
		network.UpdateChannelAnchors(orderer, "testchannel")
		network.VerifyMembership(
			network.PeersWithChannel("testchannel"),
			"testchannel",
		)
		nwo.EnableCapabilities(
			network,
			"testchannel",
			"Application", "V2_0",
			orderer,
			network.PeersWithChannel("testchannel")...,
		)

		chaincode := nwo.Chaincode{
			Name:            "gatewaycc",
			Version:         "0.0",
			Path:            components.Build("github.com/hyperledger/fabric/integration/chaincode/simple/cmd"),
			Lang:            "binary",
			PackageFile:     filepath.Join(testDir, "gatewaycc.tar.gz"),
			Ctor:            `{"Args":[]}`,
			SignaturePolicy: `AND ('Org1MSP.peer')`,
			Sequence:        "1",
			InitRequired:    false,
			Label:           "gatewaycc_label",
		}

		nwo.DeployChaincode(network, "testchannel", orderer, chaincode)

		org1Peer0 = network.Peer("Org1", "peer0")

		conn = network.PeerClientConn(org1Peer0)
		gatewayClient = gateway.NewGatewayClient(conn)
		ctx, cancel = context.WithTimeout(context.Background(), network.EventuallyTimeout)

		signingIdentity = network.PeerUserSigner(org1Peer0, "User1")
	})

	AfterEach(func() {
		conn.Close()
		cancel()

		if process != nil {
			process.Signal(syscall.SIGTERM)
			Eventually(process.Wait(), network.EventuallyTimeout).Should(Receive())
		}
		if network != nil {
			network.Cleanup()
		}
		os.RemoveAll(testDir)
	})

	submitTransaction := func(transactionName string, args ...[]byte) (*peer.Response, string) {
		proposedTransaction, transactionID := NewProposedTransaction(signingIdentity, "testchannel", "gatewaycc", transactionName, nil, args...)

		endorseRequest := &gateway.EndorseRequest{
			TransactionId:       transactionID,
			ChannelId:           "testchannel",
			ProposedTransaction: proposedTransaction,
		}

		endorseResponse, err := gatewayClient.Endorse(ctx, endorseRequest)
		Expect(err).NotTo(HaveOccurred())

		preparedTransaction := endorseResponse.GetPreparedTransaction()
		preparedTransaction.Signature, err = signingIdentity.Sign(preparedTransaction.Payload)
		Expect(err).NotTo(HaveOccurred())

		submitRequest := &gateway.SubmitRequest{
			TransactionId:       transactionID,
			ChannelId:           "testchannel",
			PreparedTransaction: preparedTransaction,
		}
		_, err = gatewayClient.Submit(ctx, submitRequest)
		Expect(err).NotTo(HaveOccurred())

		return endorseResponse.Result, transactionID
	}

	commitStatus := func(transactionID string, identity func() ([]byte, error), sign func(msg []byte) ([]byte, error)) (*gateway.CommitStatusResponse, error) {
		idBytes, err := identity()
		Expect(err).NotTo(HaveOccurred())

		statusRequest := &gateway.CommitStatusRequest{
			ChannelId:     "testchannel",
			Identity:      idBytes,
			TransactionId: transactionID,
		}
		statusRequestBytes, err := proto.Marshal(statusRequest)
		Expect(err).NotTo(HaveOccurred())

		signature, err := sign(statusRequestBytes)
		Expect(err).NotTo(HaveOccurred())

		signedStatusRequest := &gateway.SignedCommitStatusRequest{
			Request:   statusRequestBytes,
			Signature: signature,
		}

		return gatewayClient.CommitStatus(ctx, signedStatusRequest)
	}

	Describe("Evaluate", func() {
		It("should respond with the expected result", func() {
			proposedTransaction, transactionID := NewProposedTransaction(signingIdentity, "testchannel", "gatewaycc", "respond", nil, []byte("200"), []byte("conga message"), []byte("conga payload"))

			request := &gateway.EvaluateRequest{
				TransactionId:       transactionID,
				ChannelId:           "testchannel",
				ProposedTransaction: proposedTransaction,
			}

			response, err := gatewayClient.Evaluate(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			expectedResponse := &gateway.EvaluateResponse{
				Result: &peer.Response{
					Status:  200,
					Message: "conga message",
					Payload: []byte("conga payload"),
				},
			}
			Expect(response.Result.Payload).To(Equal(expectedResponse.Result.Payload))
			Expect(proto.Equal(response, expectedResponse)).To(BeTrue(), "Expected\n\t%#v\nto proto.Equal\n\t%#v", response, expectedResponse)
		})
	})

	Describe("Submit", func() {
		It("should respond with the expected result", func() {
			result, _ := submitTransaction("respond", []byte("200"), []byte("conga message"), []byte("conga payload"))
			expectedResult := &peer.Response{
				Status:  200,
				Message: "conga message",
				Payload: []byte("conga payload"),
			}
			Expect(result.Payload).To(Equal(expectedResult.Payload))
			Expect(proto.Equal(result, expectedResult)).To(BeTrue(), "Expected\n\t%#v\nto proto.Equal\n\t%#v", result, expectedResult)
		})
	})

	Describe("CommitStatus", func() {
		It("should respond with status of submitted transaction", func() {
			_, transactionID := submitTransaction("respond", []byte("200"), []byte("conga message"), []byte("conga payload"))
			status, err := commitStatus(transactionID, signingIdentity.Serialize, signingIdentity.Sign)
			Expect(err).NotTo(HaveOccurred())

			Expect(status.Result).To(Equal(peer.TxValidationCode_VALID))
		})

		It("should respond with block number", func() {
			_, transactionID := submitTransaction("respond", []byte("200"), []byte("conga message"), []byte("conga payload"))
			firstStatus, err := commitStatus(transactionID, signingIdentity.Serialize, signingIdentity.Sign)
			Expect(err).NotTo(HaveOccurred())

			_, transactionID = submitTransaction("respond", []byte("200"), []byte("conga message"), []byte("conga payload"))
			nextStatus, err := commitStatus(transactionID, signingIdentity.Serialize, signingIdentity.Sign)
			Expect(err).NotTo(HaveOccurred())

			Expect(nextStatus.BlockNumber).To(Equal(firstStatus.BlockNumber + 1))
		})

		It("should fail on unauthorized identity", func() {
			_, transactionID := submitTransaction("respond", []byte("200"), []byte("conga message"), []byte("conga payload"))
			badIdentity := network.OrdererUserSigner(network.Orderer("orderer"), "Admin")
			_, err := commitStatus(transactionID, badIdentity.Serialize, signingIdentity.Sign)
			Expect(err).To(HaveOccurred())

			grpcErr, _ := status.FromError(err)
			Expect(grpcErr.Code()).To(Equal(codes.PermissionDenied))
		})

		It("should fail on bad signature", func() {
			_, transactionID := submitTransaction("respond", []byte("200"), []byte("conga message"), []byte("conga payload"))
			badSign := func(digest []byte) ([]byte, error) {
				return signingIdentity.Sign([]byte("WRONG"))
			}
			_, err := commitStatus(transactionID, signingIdentity.Serialize, badSign)
			Expect(err).To(HaveOccurred())

			grpcErr, _ := status.FromError(err)
			Expect(grpcErr.Code()).To(Equal(codes.PermissionDenied))
		})
	})

	Describe("ChaincodeEvents", func() {
		It("should respond with emitted chaincode events", func() {
			identityBytes, err := signingIdentity.Serialize()
			Expect(err).NotTo(HaveOccurred())

			request := &gateway.ChaincodeEventsRequest{
				ChannelId:   "testchannel",
				ChaincodeId: "gatewaycc",
				Identity:    identityBytes,
			}

			requestBytes, err := proto.Marshal(request)
			Expect(err).NotTo(HaveOccurred())

			signature, err := signingIdentity.Sign(requestBytes)
			Expect(err).NotTo(HaveOccurred())

			signedRequest := &gateway.SignedChaincodeEventsRequest{
				Request:   requestBytes,
				Signature: signature,
			}

			eventCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			eventsClient, err := gatewayClient.ChaincodeEvents(eventCtx, signedRequest)
			Expect(err).NotTo(HaveOccurred())

			_, transactionID := submitTransaction("event", []byte("EVENT_NAME"), []byte("EVENT_PAYLOAD"))

			event, err := eventsClient.Recv()
			Expect(err).NotTo(HaveOccurred())

			Expect(event.Events).To(HaveLen(1), "number of events")
			expectedEvent := &peer.ChaincodeEvent{
				ChaincodeId: "gatewaycc",
				TxId:        transactionID,
				EventName:   "EVENT_NAME",
				Payload:     []byte("EVENT_PAYLOAD"),
			}
			Expect(proto.Equal(event.Events[0], expectedEvent)).To(BeTrue(), "Expected\n\t%#v\nto proto.Equal\n\t%#v", event.Events[0], expectedEvent)
		})
	})
})
