import { iif, MonoTypeOperatorFunction, of, Subscriber, throwError } from 'rxjs';
import { concatMap, delay, retryWhen } from 'rxjs/operators';

/**
* ReplyType contains all possible message types of a reply.
*/
export type ReplyType = 'ok'
  | 'upd'
  | 'new'
  | 'del'
  | 'success'
  | 'error'
  | 'warning'
  | 'done';

/**
* RequestType contains all possible message types of a request.
*/
export type RequestType = 'get'
  | 'query'
  | 'sub'
  | 'qsub'
  | 'create'
  | 'update'
  | 'insert'
  | 'delete'
  | 'cancel';

// RecordMeta describes the meta-data object that is part of
// every API resource.
export interface RecordMeta {
  // Created hold a unix-epoch timestamp when the record has been
  // created.
  Created: number;
  // Deleted hold a unix-epoch timestamp when the record has been
  // deleted.
  Deleted: number;
  // Expires hold a unix-epoch timestamp when the record has been
  // expires.
  Expires: number;
  // Modified hold a unix-epoch timestamp when the record has been
  // modified last.
  Modified: number;
  // Key holds the database record key.
  Key: string;
}

export interface Process extends Record {
  Name: string;
  UserID: number;
  UserName: string;
  UserHome: string;
  Pid: number;
  Pgid: number;
  CreatedAt: number;
  ParentPid: number;
  ParentCreatedAt: number;
  Path: string;
  ExecName: string;
  Cwd: string;
  CmdLine: string;
  FirstArg: string;
  Env: {
    [key: string]: string
  } | null;
  Tags: {
    Key: string;
    Value: string;
  }[] | null;
  MatchingPath: string;
  PrimaryProfileID: string;
  FirstSeen: number;
  LastSeen: number;
  Error: string;
  ExecHashes: {
    [key: string]: string
  } | null;
}

// Record describes the base record structure of all API resources.
export interface Record {
  _meta?: RecordMeta;
}

/**
* All possible MessageType that are available in PortAPI.
*/
export type MessageType = RequestType | ReplyType;

/**
* BaseMessage describes the base message type that is exchanged
* via PortAPI.
*/
export interface BaseMessage<M extends MessageType = MessageType> {
  // ID of the request. Used to correlated (multiplex) requests and
  // responses across a single websocket connection.
  id: string;
  // Type is the request/response message type.
  type: M;
}

/**
* DoneReply marks the end of a PortAPI stream.
*/
export interface DoneReply extends BaseMessage<'done'> { }

/**
* DataReply is either sent once as a result on a `get` request or
* is sent multiple times in the course of a PortAPI stream.
*/
export interface DataReply<T extends Record> extends BaseMessage<'ok' | 'upd' | 'new' | 'del'> {
  // Key is the database key including the database prefix.
  key: string;
  // Data is the actual data of the entry.
  data: T;
}

/**
 * Returns true if d is a DataReply message type.
 *
 * @param d The reply message to check
 */
export function isDataReply(d: ReplyMessage): d is DataReply<any> {
  return d.type === 'ok'
    || d.type === 'upd'
    || d.type === 'new'
    || d.type === 'del';
  //|| d.type === 'done'; // done is actually not correct
}

/**
* SuccessReply is used to mark an operation as successfully. It does not carry any
* data. Think of it as a "201 No Content" in HTTP.
*/
export interface SuccessReply extends BaseMessage<'success'> { }

/**
* ErrorReply describes an error that happened while processing a
* request. Note that an `error` type message may be sent for single
* and response-stream requests. In case of a stream the `error` type
* message marks the end of the stream. See WarningReply for a simple
* warning message that can be transmitted via PortAPI.
*/
export interface ErrorReply extends BaseMessage<'error'> {
  // Message is the error message from the backend.
  message: string;
}

/**
* WarningReply contains a warning message that describes an error
* condition encountered when processing a single entitiy of a
* response stream. In contrast to `error` type messages, a `warning`
* can only occure during data streams and does not end the stream.
*/
export interface WarningReply extends BaseMessage<'warning'> {
  // Message describes the warning/error condition the backend
  // encountered.
  message: string;
}

/**
* QueryRequest defines the payload for `query`, `sub` and `qsub` message
* types. The result of a query request is always a stream of responses.
* See ErrorReply, WarningReply and DoneReply for more information.
*/
export interface QueryRequest extends BaseMessage<'query' | 'sub' | 'qsub'> {
  // Query is the query for the database.
  query: string;
}

/**
* KeyRequests defines the payload for a `get` or `delete` request. Those
* message type only carry the key of the database entry to delete. Note that
* `delete` can only return a `success` or `error` type message while `get` will
* receive a `ok` or `error` type message.
*/
export interface KeyRequest extends BaseMessage<'delete' | 'get'> {
  // Key is the database entry key.
  key: string;
}


/**
* DataRequest is used during create, insert or update operations.
* TODO(ppacher): check what's the difference between create and insert,
*                both seem to error when trying to create a new entry.
*/
export interface DataRequest<T> extends BaseMessage<'update' | 'create' | 'insert'> {
  // Key is the database entry key.
  key: string;
  // Data is the data to store.
  data: T;
}

/**
 * CancelRequest can be sent on stream operations to early-abort the request.
 */
export interface CancelRequest extends BaseMessage<'cancel'> { }

/**
* ReplyMessage is a union of all reply message types.
*/
export type ReplyMessage<T extends Record = any> = DataReply<T>
  | DoneReply
  | SuccessReply
  | WarningReply
  | ErrorReply;

/**
* RequestMessage is a union of all request message types.
*/
export type RequestMessage<T = any> = QueryRequest
  | KeyRequest
  | DataRequest<T>
  | CancelRequest;

/**
* Requestable can be used to accept only properties that match
* the request message type M.
*/
export type Requestable<M extends RequestType> = RequestMessage & { type: M };

/**
 * Returns true if m is a cancellable message type.
 *
 * @param m The message type to check.
 */
export function isCancellable(m: MessageType): boolean {
  switch (m) {
    case 'qsub':
    case 'sub':
      return true;
    default:
      return false;
  }
}

/**
 * Reflects a currently in-flight PortAPI request. Used to
 * intercept and mangle with responses.
 */
export interface InspectedActiveRequest {
  // The type of request.
  type: RequestType;
  // The actual request payload.
  // @todo(ppacher): typings
  payload: any;
  // The request observer. Use to inject data
  // or complete/error the subscriber. Use with
  // care!
  observer: Subscriber<DataReply<any>>;
  // Counter for the number of messages received
  // for this request.
  messagesReceived: number;
  // The last data received on the request
  lastData: any;
  // The last key received on the request
  lastKey: string;
}

export interface RetryableOpts {
  // A delay in milliseconds before retrying an operation.
  retryDelay?: number;
  // The maximum number of retries.
  maxRetries?: number;
}

export interface ProfileImportResult extends ImportResult {
  replacesProfiles: string[];
}

export interface ImportResult {
  restartRequired: boolean;
  replacesExisting: boolean;
  containsUnknown: boolean;
}

/**
 * Returns a RxJS operator function that implements a retry pipeline
 * with a configurable retry delay and an optional maximum retry count.
 * If maxRetries is reached the last error captured is thrown.
 *
 * @param opts  Configuration options for the retryPipeline.
 *        see {@type RetryableOpts} for more information.
 */
export function retryPipeline<T>({ retryDelay, maxRetries }: RetryableOpts = {}): MonoTypeOperatorFunction<T> {
  return retryWhen(errors => errors.pipe(
    // use concatMap to keep the errors in order and make sure
    // they don't execute in parallel.
    concatMap((e, i) =>
      iif(
        // conditional observable seletion, throwError if i > maxRetries
        // or a retryDelay otherwise
        () => i > (maxRetries || Infinity),
        throwError(() => e),
        of(e).pipe(delay(retryDelay || 1000))
      )
    )
  ))
}

export interface WatchOpts extends RetryableOpts {
  // Whether or not `new` updates should be filtered
  // or let through. See {@method PortAPI.watch} for
  // more information.
  ingoreNew?: boolean;

  ignoreDelete?: boolean;
}


/**
* Serializes a request or reply message into it's wire format.
*
* @param msg The request or reply messsage to serialize
*/
export function serializeMessage(msg: RequestMessage | ReplyMessage): any {
  if (msg === undefined) {
    return undefined;
  }

  let blob = `${msg.id}|${msg.type}`;

  switch (msg.type) {
    case 'done':        // reply
    case 'success':     // reply
    case 'cancel':      // request
      break;

    case 'error':       // reply
    case 'warning':     // reply
      blob += `|${msg.message}`
      break;

    case 'ok':          // reply
    case 'upd':         // reply
    case 'new':         // reply
    case 'insert':      // request
    case 'update':      // request
    case 'create':      // request
      blob += `|${msg.key}|J${JSON.stringify(msg.data)}`
      break;


    case 'del':         // reply
    case 'get':         // request
    case 'delete':      // request
      blob += `|${msg.key}`
      break;

    case 'query':       // request
    case 'sub':         // request
    case 'qsub':        // request
      blob += `|query ${msg.query}`
      break;

    default:
      // We need (msg as any) here because typescript knows that we covered
      // all possible values above and that .type can never be something else.
      // Still, we want to guard against unexpected portmaster message
      // types.
      console.error(`Unknown message type ${(msg as any).type}`);
  }

  return blob;
}

/**
* Deserializes (loads) a PortAPI message from a WebSocket message event.
*
* @param event The WebSocket MessageEvent to parse.
*/
export function deserializeMessage(event: MessageEvent): RequestMessage | ReplyMessage {
  let data: string;

  if (typeof event.data !== 'string') {
    data = new TextDecoder("utf-8").decode(event.data)
  } else {
    data = event.data;
  }

  const parts = data.split("|");

  if (parts.length < 2) {
    throw new Error(`invalid number of message parts, expected 3-4 but got ${parts.length}`);
  }

  const id = parts[0];
  const type = parts[1] as MessageType;

  var msg: Partial<RequestMessage | ReplyMessage> = {
    id,
    type,
  }

  if (parts.length > 4) {
    parts[3] = parts.slice(3).join('|')
  }

  switch (msg.type) {
    case 'done':        // reply
    case 'success':     // reply
    case 'cancel':      // request
      break;

    case 'error':       // reply
    case 'warning':     // reply
      msg.message = parts[2];
      break;

    case 'ok':          // reply
    case 'upd':         // reply
    case 'new':         // reply
    case 'insert':      // request
    case 'update':      // request
    case 'create':      // request
      msg.key = parts[2];
      try {
        if (parts[3][0] === 'J') {
          msg.data = JSON.parse(parts[3].slice(1));
        } else {
          msg.data = parts[3];
        }
      } catch (e) {
        console.log(e, data)
      }
      break;

    case 'del':         // reply
    case 'get':         // request
    case 'delete':      // request
      msg.key = parts[2];
      break;

    case 'query':       // request
    case 'sub':         // request
    case 'qsub':        // request
      msg.query = parts[2];
      if (msg.query.startsWith("query ")) {
        msg.query = msg.query.slice(6);
      }
      break;

    default:
      // We need (msg as any) here because typescript knows that we covered
      // all possible values above and that .type can never be something else.
      // Still, we want to guard against unexpected portmaster message
      // types.
      console.error(`Unknown message type ${(msg as any).type}`);
  }

  return msg as (ReplyMessage | RequestMessage); // it's not partitial anymore
}
