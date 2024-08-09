package api

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/tevino/abool"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
	"github.com/safing/structures/varint"
)

const (
	dbMsgTypeOk      = "ok"
	dbMsgTypeError   = "error"
	dbMsgTypeDone    = "done"
	dbMsgTypeSuccess = "success"
	dbMsgTypeUpd     = "upd"
	dbMsgTypeNew     = "new"
	dbMsgTypeDel     = "del"
	dbMsgTypeWarning = "warning"

	dbAPISeperator = "|"
	emptyString    = ""
)

var (
	dbAPISeperatorBytes       = []byte(dbAPISeperator)
	dbCompatibilityPermission = PermitAdmin
)

func init() {
	RegisterHandler("/api/database/v1", WrapInAuthHandler(
		startDatabaseWebsocketAPI,
		// Default to admin read/write permissions until the database gets support
		// for api permissions.
		dbCompatibilityPermission,
		dbCompatibilityPermission,
	))
}

// DatabaseAPI is a generic database API interface.
type DatabaseAPI struct {
	queriesLock sync.Mutex
	queries     map[string]*iterator.Iterator

	subsLock sync.Mutex
	subs     map[string]*database.Subscription

	shutdownSignal chan struct{}
	shuttingDown   *abool.AtomicBool
	db             *database.Interface

	sendBytes func(data []byte)
}

// DatabaseWebsocketAPI is a database websocket API interface.
type DatabaseWebsocketAPI struct {
	DatabaseAPI

	sendQueue chan []byte
	conn      *websocket.Conn
}

func allowAnyOrigin(r *http.Request) bool {
	return true
}

// CreateDatabaseAPI creates a new database interface.
func CreateDatabaseAPI(sendFunction func(data []byte)) DatabaseAPI {
	return DatabaseAPI{
		queries:        make(map[string]*iterator.Iterator),
		subs:           make(map[string]*database.Subscription),
		shutdownSignal: make(chan struct{}),
		shuttingDown:   abool.NewBool(false),
		db:             database.NewInterface(nil),
		sendBytes:      sendFunction,
	}
}

func startDatabaseWebsocketAPI(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin:     allowAnyOrigin,
		ReadBufferSize:  1024,
		WriteBufferSize: 65536,
	}
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		errMsg := fmt.Sprintf("could not upgrade: %s", err)
		log.Error(errMsg)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	newDBAPI := &DatabaseWebsocketAPI{
		DatabaseAPI: DatabaseAPI{
			queries:        make(map[string]*iterator.Iterator),
			subs:           make(map[string]*database.Subscription),
			shutdownSignal: make(chan struct{}),
			shuttingDown:   abool.NewBool(false),
			db:             database.NewInterface(nil),
		},

		sendQueue: make(chan []byte, 100),
		conn:      wsConn,
	}

	newDBAPI.sendBytes = func(data []byte) {
		newDBAPI.sendQueue <- data
	}

	module.mgr.Go("database api handler", newDBAPI.handler)
	module.mgr.Go("database api writer", newDBAPI.writer)

	log.Tracer(r.Context()).Infof("api request: init websocket %s %s", r.RemoteAddr, r.RequestURI)
}

func (api *DatabaseWebsocketAPI) handler(_ *mgr.WorkerCtx) error {
	defer func() {
		_ = api.shutdown(nil)
	}()

	for {
		_, msg, err := api.conn.ReadMessage()
		if err != nil {
			return api.shutdown(err)
		}

		api.Handle(msg)
	}
}

func (api *DatabaseWebsocketAPI) writer(ctx *mgr.WorkerCtx) error {
	defer func() {
		_ = api.shutdown(nil)
	}()

	var data []byte
	var err error

	for {
		select {
		// prioritize direct writes
		case data = <-api.sendQueue:
			if len(data) == 0 {
				return nil
			}
		case <-ctx.Done():
			return nil
		case <-api.shutdownSignal:
			return nil
		}

		// log.Tracef("api: sending %s", string(*msg))
		err = api.conn.WriteMessage(websocket.BinaryMessage, data)
		if err != nil {
			return api.shutdown(err)
		}
	}
}

func (api *DatabaseWebsocketAPI) shutdown(err error) error {
	// Check if we are the first to shut down.
	if !api.shuttingDown.SetToIf(false, true) {
		return nil
	}

	// Check the given error.
	if err != nil {
		if websocket.IsCloseError(err,
			websocket.CloseNormalClosure,
			websocket.CloseGoingAway,
			websocket.CloseAbnormalClosure,
		) {
			log.Infof("api: websocket connection to %s closed", api.conn.RemoteAddr())
		} else {
			log.Warningf("api: websocket connection error with %s: %s", api.conn.RemoteAddr(), err)
		}
	}

	// Trigger shutdown.
	close(api.shutdownSignal)
	_ = api.conn.Close()
	return nil
}

// Handle handles a message for the database API.
func (api *DatabaseAPI) Handle(msg []byte) {
	// 123|get|<key>
	//    123|ok|<key>|<data>
	//    123|error|<message>
	// 124|query|<query>
	//    124|ok|<key>|<data>
	//    124|done
	//    124|error|<message>
	//    124|warning|<message> // error with single record, operation continues
	// 124|cancel
	// 125|sub|<query>
	//    125|upd|<key>|<data>
	//    125|new|<key>|<data>
	//    127|del|<key>
	//    125|warning|<message> // error with single record, operation continues
	// 125|cancel
	// 127|qsub|<query>
	//    127|ok|<key>|<data>
	//    127|done
	//    127|error|<message>
	//    127|upd|<key>|<data>
	//    127|new|<key>|<data>
	//    127|del|<key>
	//    127|warning|<message> // error with single record, operation continues
	// 127|cancel

	// 128|create|<key>|<data>
	//    128|success
	//    128|error|<message>
	// 129|update|<key>|<data>
	//    129|success
	//    129|error|<message>
	// 130|insert|<key>|<data>
	//    130|success
	//    130|error|<message>
	// 131|delete|<key>
	//    131|success
	//    131|error|<message>

	parts := bytes.SplitN(msg, []byte("|"), 3)

	// Handle special command "cancel"
	if len(parts) == 2 && string(parts[1]) == "cancel" {
		// 124|cancel
		// 125|cancel
		// 127|cancel
		go api.handleCancel(parts[0])
		return
	}

	if len(parts) != 3 {
		api.send(nil, dbMsgTypeError, "bad request: malformed message", nil)
		return
	}

	switch string(parts[1]) {
	case "get":
		// 123|get|<key>
		go api.handleGet(parts[0], string(parts[2]))
	case "query":
		// 124|query|<query>
		go api.handleQuery(parts[0], string(parts[2]))
	case "sub":
		// 125|sub|<query>
		go api.handleSub(parts[0], string(parts[2]))
	case "qsub":
		// 127|qsub|<query>
		go api.handleQsub(parts[0], string(parts[2]))
	case "create", "update", "insert":
		// split key and payload
		dataParts := bytes.SplitN(parts[2], []byte("|"), 2)
		if len(dataParts) != 2 {
			api.send(nil, dbMsgTypeError, "bad request: malformed message", nil)
			return
		}

		switch string(parts[1]) {
		case "create":
			// 128|create|<key>|<data>
			go api.handlePut(parts[0], string(dataParts[0]), dataParts[1], true)
		case "update":
			// 129|update|<key>|<data>
			go api.handlePut(parts[0], string(dataParts[0]), dataParts[1], false)
		case "insert":
			// 130|insert|<key>|<data>
			go api.handleInsert(parts[0], string(dataParts[0]), dataParts[1])
		}
	case "delete":
		// 131|delete|<key>
		go api.handleDelete(parts[0], string(parts[2]))
	default:
		api.send(parts[0], dbMsgTypeError, "bad request: unknown method", nil)
	}
}

func (api *DatabaseAPI) send(opID []byte, msgType string, msgOrKey string, data []byte) {
	c := container.New(opID)
	c.Append(dbAPISeperatorBytes)
	c.Append([]byte(msgType))

	if msgOrKey != emptyString {
		c.Append(dbAPISeperatorBytes)
		c.Append([]byte(msgOrKey))
	}

	if len(data) > 0 {
		c.Append(dbAPISeperatorBytes)
		c.Append(data)
	}

	api.sendBytes(c.CompileData())
}

func (api *DatabaseAPI) handleGet(opID []byte, key string) {
	// 123|get|<key>
	//    123|ok|<key>|<data>
	//    123|error|<message>

	var data []byte

	r, err := api.db.Get(key)
	if err == nil {
		data, err = MarshalRecord(r, true)
	}
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return
	}
	api.send(opID, dbMsgTypeOk, r.Key(), data)
}

func (api *DatabaseAPI) handleQuery(opID []byte, queryText string) {
	// 124|query|<query>
	//    124|ok|<key>|<data>
	//    124|done
	//    124|warning|<message>
	//    124|error|<message>
	//    124|warning|<message> // error with single record, operation continues
	// 124|cancel

	var err error

	q, err := query.ParseQuery(queryText)
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return
	}

	api.processQuery(opID, q)
}

func (api *DatabaseAPI) processQuery(opID []byte, q *query.Query) (ok bool) {
	it, err := api.db.Query(q)
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return false
	}

	// Save query iterator.
	api.queriesLock.Lock()
	api.queries[string(opID)] = it
	api.queriesLock.Unlock()

	// Remove query iterator after it ended.
	defer func() {
		api.queriesLock.Lock()
		defer api.queriesLock.Unlock()
		delete(api.queries, string(opID))
	}()

	for {
		select {
		case <-api.shutdownSignal:
			// cancel query and return
			it.Cancel()
			return false
		case r := <-it.Next:
			// process query feed
			if r != nil {
				// process record
				data, err := MarshalRecord(r, true)
				if err != nil {
					api.send(opID, dbMsgTypeWarning, err.Error(), nil)
					continue
				}
				api.send(opID, dbMsgTypeOk, r.Key(), data)
			} else {
				// sub feed ended
				if it.Err() != nil {
					api.send(opID, dbMsgTypeError, it.Err().Error(), nil)
					return false
				}
				api.send(opID, dbMsgTypeDone, emptyString, nil)
				return true
			}
		}
	}
}

// func (api *DatabaseWebsocketAPI) runQuery()

func (api *DatabaseAPI) handleSub(opID []byte, queryText string) {
	// 125|sub|<query>
	//    125|upd|<key>|<data>
	//    125|new|<key>|<data>
	//    125|delete|<key>
	//    125|warning|<message> // error with single record, operation continues
	// 125|cancel
	var err error

	q, err := query.ParseQuery(queryText)
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return
	}

	sub, ok := api.registerSub(opID, q)
	if !ok {
		return
	}
	api.processSub(opID, sub)
}

func (api *DatabaseAPI) registerSub(opID []byte, q *query.Query) (sub *database.Subscription, ok bool) {
	var err error
	sub, err = api.db.Subscribe(q)
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return nil, false
	}

	return sub, true
}

func (api *DatabaseAPI) processSub(opID []byte, sub *database.Subscription) {
	// Save subscription.
	api.subsLock.Lock()
	api.subs[string(opID)] = sub
	api.subsLock.Unlock()

	// Remove subscription after it ended.
	defer func() {
		api.subsLock.Lock()
		defer api.subsLock.Unlock()
		delete(api.subs, string(opID))
	}()

	for {
		select {
		case <-api.shutdownSignal:
			// cancel sub and return
			_ = sub.Cancel()
			return
		case r := <-sub.Feed:
			// process sub feed
			if r != nil {
				// process record
				data, err := MarshalRecord(r, true)
				if err != nil {
					api.send(opID, dbMsgTypeWarning, err.Error(), nil)
					continue
				}
				// TODO: use upd, new and delete msgTypes
				r.Lock()
				isDeleted := r.Meta().IsDeleted()
				isNew := r.Meta().Created == r.Meta().Modified
				r.Unlock()
				switch {
				case isDeleted:
					api.send(opID, dbMsgTypeDel, r.Key(), nil)
				case isNew:
					api.send(opID, dbMsgTypeNew, r.Key(), data)
				default:
					api.send(opID, dbMsgTypeUpd, r.Key(), data)
				}
			} else {
				// sub feed ended
				api.send(opID, dbMsgTypeDone, "", nil)
				return
			}
		}
	}
}

func (api *DatabaseAPI) handleQsub(opID []byte, queryText string) {
	// 127|qsub|<query>
	//    127|ok|<key>|<data>
	//    127|done
	//    127|error|<message>
	//    127|upd|<key>|<data>
	//    127|new|<key>|<data>
	//    127|delete|<key>
	//    127|warning|<message> // error with single record, operation continues
	// 127|cancel

	var err error

	q, err := query.ParseQuery(queryText)
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return
	}

	sub, ok := api.registerSub(opID, q)
	if !ok {
		return
	}
	ok = api.processQuery(opID, q)
	if !ok {
		return
	}
	api.processSub(opID, sub)
}

func (api *DatabaseAPI) handleCancel(opID []byte) {
	api.cancelQuery(opID)
	api.cancelSub(opID)
}

func (api *DatabaseAPI) cancelQuery(opID []byte) {
	api.queriesLock.Lock()
	defer api.queriesLock.Unlock()

	// Get subscription from api.
	it, ok := api.queries[string(opID)]
	if !ok {
		// Fail silently as quries end by themselves when finished.
		return
	}

	// End query.
	it.Cancel()

	// The query handler will end the communication with a done message.
}

func (api *DatabaseAPI) cancelSub(opID []byte) {
	api.subsLock.Lock()
	defer api.subsLock.Unlock()

	// Get subscription from api.
	sub, ok := api.subs[string(opID)]
	if !ok {
		api.send(opID, dbMsgTypeError, "could not find subscription", nil)
		return
	}

	// End subscription.
	err := sub.Cancel()
	if err != nil {
		api.send(opID, dbMsgTypeError, fmt.Sprintf("failed to cancel subscription: %s", err), nil)
	}

	// The subscription handler will end the communication with a done message.
}

func (api *DatabaseAPI) handlePut(opID []byte, key string, data []byte, create bool) {
	// 128|create|<key>|<data>
	//    128|success
	//    128|error|<message>

	// 129|update|<key>|<data>
	//    129|success
	//    129|error|<message>

	if len(data) < 2 {
		api.send(opID, dbMsgTypeError, "bad request: malformed message", nil)
		return
	}

	// TODO - staged for deletion: remove transition code
	// if data[0] != dsd.JSON {
	// 	typedData := make([]byte, len(data)+1)
	// 	typedData[0] = dsd.JSON
	// 	copy(typedData[1:], data)
	// 	data = typedData
	// }

	r, err := record.NewWrapper(key, nil, data[0], data[1:])
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return
	}

	if create {
		err = api.db.PutNew(r)
	} else {
		err = api.db.Put(r)
	}
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return
	}
	api.send(opID, dbMsgTypeSuccess, emptyString, nil)
}

func (api *DatabaseAPI) handleInsert(opID []byte, key string, data []byte) {
	// 130|insert|<key>|<data>
	//    130|success
	//    130|error|<message>

	r, err := api.db.Get(key)
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return
	}

	acc := r.GetAccessor(r)

	result := gjson.ParseBytes(data)
	anythingPresent := false
	var insertError error
	result.ForEach(func(key gjson.Result, value gjson.Result) bool {
		anythingPresent = true
		if !key.Exists() {
			insertError = errors.New("values must be in a map")
			return false
		}
		if key.Type != gjson.String {
			insertError = errors.New("keys must be strings")
			return false
		}
		if !value.Exists() {
			insertError = errors.New("non-existent value")
			return false
		}
		insertError = acc.Set(key.String(), value.Value())
		return insertError == nil
	})

	if insertError != nil {
		api.send(opID, dbMsgTypeError, insertError.Error(), nil)
		return
	}
	if !anythingPresent {
		api.send(opID, dbMsgTypeError, "could not find any valid values", nil)
		return
	}

	err = api.db.Put(r)
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return
	}

	api.send(opID, dbMsgTypeSuccess, emptyString, nil)
}

func (api *DatabaseAPI) handleDelete(opID []byte, key string) {
	// 131|delete|<key>
	//    131|success
	//    131|error|<message>

	err := api.db.Delete(key)
	if err != nil {
		api.send(opID, dbMsgTypeError, err.Error(), nil)
		return
	}
	api.send(opID, dbMsgTypeSuccess, emptyString, nil)
}

// MarshalRecord locks and marshals the given record, additionally adding
// metadata and returning it as json.
func MarshalRecord(r record.Record, withDSDIdentifier bool) ([]byte, error) {
	r.Lock()
	defer r.Unlock()

	// Pour record into JSON.
	jsonData, err := r.Marshal(r, dsd.JSON)
	if err != nil {
		return nil, err
	}

	// Remove JSON identifier for manual editing.
	jsonData = bytes.TrimPrefix(jsonData, varint.Pack8(dsd.JSON))

	// Add metadata.
	jsonData, err = sjson.SetBytes(jsonData, "_meta", r.Meta())
	if err != nil {
		return nil, err
	}

	// Add database key.
	jsonData, err = sjson.SetBytes(jsonData, "_meta.Key", r.Key())
	if err != nil {
		return nil, err
	}

	// Add JSON identifier again.
	if withDSDIdentifier {
		formatID := varint.Pack8(dsd.JSON)
		finalData := make([]byte, 0, len(formatID)+len(jsonData))
		finalData = append(finalData, formatID...)
		finalData = append(finalData, jsonData...)
		return finalData, nil
	}
	return jsonData, nil
}
