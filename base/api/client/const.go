package client

// Message Types.
const (
	msgRequestGet    = "get"
	msgRequestQuery  = "query"
	msgRequestSub    = "sub"
	msgRequestQsub   = "qsub"
	msgRequestCreate = "create"
	msgRequestUpdate = "update"
	msgRequestInsert = "insert"
	msgRequestDelete = "delete"

	MsgOk      = "ok"
	MsgError   = "error"
	MsgDone    = "done"
	MsgSuccess = "success"
	MsgUpdate  = "upd"
	MsgNew     = "new"
	MsgDelete  = "del"
	MsgWarning = "warning"

	MsgOffline = "offline" // special message type for signaling the handler that the connection was lost

	apiSeperator = "|"
)

var apiSeperatorBytes = []byte(apiSeperator)
