/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package messaging

import (
	"fmt"
	"net/http"

	"github.com/hyperledger/aries-framework-go/pkg/controller/command"
	"github.com/hyperledger/aries-framework-go/pkg/controller/command/messaging"
	"github.com/hyperledger/aries-framework-go/pkg/controller/internal/cmdutil"
	"github.com/hyperledger/aries-framework-go/pkg/controller/rest"
	"github.com/hyperledger/aries-framework-go/pkg/controller/webhook"
	"github.com/hyperledger/aries-framework-go/pkg/didcomm/dispatcher"
	"github.com/hyperledger/aries-framework-go/pkg/kms/legacykms"
	"github.com/hyperledger/aries-framework-go/pkg/storage"
)

const (
	// http over didcomm endpoints
	httpOverDIDComm                = "/http-over-didcomm"
	registerHTTPOverDIDCommService = httpOverDIDComm + "/register"

	// message service endpoints
	msgServiceOperationID = "/message"
	registerMsgService    = msgServiceOperationID + "/register-service"
	unregisterMsgService  = msgServiceOperationID + "/unregister-service"
	msgServiceList        = msgServiceOperationID + "/services"
	sendNewMsg            = msgServiceOperationID + "/send"
	sendReplyMsg          = msgServiceOperationID + "/reply"
)

// provider contains dependencies for the common controller operations
// and is typically created by using aries.Context()
type provider interface {
	OutboundDispatcher() dispatcher.Outbound
	TransientStorageProvider() storage.Provider
	StorageProvider() storage.Provider
	LegacyKMS() legacykms.KeyManager
}

// Operation contains basic common operations provided by controller REST API
type Operation struct {
	command  *messaging.Command
	handlers []rest.Handler
}

// New returns new common operations rest client instance
func New(ctx provider, registrar command.MessageHandler, notifier webhook.Notifier) (*Operation, error) {
	msgcmd, err := messaging.New(ctx, registrar, notifier)
	if err != nil {
		return nil, fmt.Errorf("failed to create messaging controller command: %w", err)
	}

	o := &Operation{command: msgcmd}
	o.registerHandler()

	return o, nil
}

// GetRESTHandlers get all controller API handler available for this service
func (o *Operation) GetRESTHandlers() []rest.Handler {
	return o.handlers
}

// registerHandler register handlers to be exposed from this protocol service as REST API endpoints
func (o *Operation) registerHandler() {
	// Add more protocol endpoints here to expose them as controller API endpoints
	o.handlers = []rest.Handler{
		cmdutil.NewHTTPHandler(registerMsgService, http.MethodPost, o.RegisterService),
		cmdutil.NewHTTPHandler(unregisterMsgService, http.MethodPost, o.UnregisterService),
		cmdutil.NewHTTPHandler(msgServiceList, http.MethodGet, o.Services),
		cmdutil.NewHTTPHandler(sendNewMsg, http.MethodPost, o.Send),
		cmdutil.NewHTTPHandler(sendReplyMsg, http.MethodPost, o.Reply),
		cmdutil.NewHTTPHandler(registerHTTPOverDIDCommService, http.MethodPost, o.RegisterHTTPService),
	}
}

// RegisterService swagger:route POST /message/register-service message registerMsgSvc
//
// registers new message service to message handler registrar
//
// Responses:
//    default: genericError
func (o *Operation) RegisterService(rw http.ResponseWriter, req *http.Request) {
	rest.Execute(o.command.RegisterService, rw, req.Body)
}

// UnregisterService swagger:route POST /message/unregister-service message http-over-didcomm unregisterMsgSvc
//
// unregisters given message service handler registrar
//
// Responses:
//    default: genericError
func (o *Operation) UnregisterService(rw http.ResponseWriter, req *http.Request) {
	rest.Execute(o.command.UnregisterService, rw, req.Body)
}

// Services swagger:route GET /message/services message http-over-didcomm services
//
// returns list of registered service names
//
// Responses:
//    default: genericError
//    200: registeredServicesResponse
func (o *Operation) Services(rw http.ResponseWriter, req *http.Request) {
	rest.Execute(o.command.Services, rw, req.Body)
}

// Send swagger:route POST /message/send message sendNewMessage
//
// sends new message to destination provided
//
// Responses:
//    default: genericError
func (o *Operation) Send(rw http.ResponseWriter, req *http.Request) {
	rest.Execute(o.command.Send, rw, req.Body)
}

// Reply swagger:route POST /message/reply message sendReplyMessage
//
// sends reply to existing message
//
// Responses:
//    default: genericError
func (o *Operation) Reply(rw http.ResponseWriter, req *http.Request) {
	rest.Execute(o.command.Reply, rw, req.Body)
}

// RegisterHTTPService swagger:route POST /http-over-didcomm/register http-over-didcomm registerHttpMsgSvc
//
// registers new http over didcomm service to message handler registrar
//
// Responses:
//    default: genericError
func (o *Operation) RegisterHTTPService(rw http.ResponseWriter, req *http.Request) {
	rest.Execute(o.command.RegisterHTTPService, rw, req.Body)
}