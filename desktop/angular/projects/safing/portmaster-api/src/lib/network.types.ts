import { Record } from './portapi.types';

export enum Verdict {
  Undecided = 0,
  Undeterminable = 1,
  Accept = 2,
  Block = 3,
  Drop = 4,
  RerouteToNs = 5,
  RerouteToTunnel = 6,
  Failed = 7
}

export enum IPProtocol {
  ICMP = 1,
  IGMP = 2,
  TCP = 6,
  UDP = 17,
  ICMPv6 = 58,
  UDPLite = 136,
  RAW = 255, // TODO(ppacher): what is RAW used for?
}

export enum IPVersion {
  V4 = 4,
  V6 = 6,
}

export enum IPScope {
  Invalid = -1,
  Undefined = 0,
  HostLocal = 1,
  LinkLocal = 2,
  SiteLocal = 3,
  Global = 4,
  LocalMulticast = 5,
  GlobalMulitcast = 6
}

let globalScopes = new Set([IPScope.GlobalMulitcast, IPScope.Global])
let localScopes = new Set([IPScope.SiteLocal, IPScope.LinkLocal, IPScope.LocalMulticast])

// IsGlobalScope returns true if scope represents a globally
// routed destination.
export function IsGlobalScope(scope: IPScope): scope is IPScope.GlobalMulitcast | IPScope.Global {
  return globalScopes.has(scope);
}

// IsLocalScope returns true if scope represents a locally
// routed destination.
export function IsLANScope(scope: IPScope): scope is IPScope.SiteLocal | IPScope.LinkLocal | IPScope.LocalMulticast {
  return localScopes.has(scope);
}

// IsLocalhost returns true if scope represents localhost.
export function IsLocalhost(scope: IPScope): scope is IPScope.HostLocal {
  return scope === IPScope.HostLocal;
}

const deniedVerdicts = new Set([
  Verdict.Drop,
  Verdict.Block,
])
// IsDenied returns true if the verdict v represents a
// deny or block decision.
export function IsDenied(v: Verdict): boolean {
  return deniedVerdicts.has(v);
}

export interface CountryInfo {
  Code: string;
	Name: string;
	Center: GeoCoordinates;
  Continent: ContinentInfo;
}

export interface ContinentInfo {
  Code: string;
  Region: string;
  Name: string;
}

export interface GeoCoordinates {
  AccuracyRadius: number;
  Latitude: number;
  Longitude: number;
}

export const UnknownLocation: GeoCoordinates = {
  AccuracyRadius: 0,
  Latitude: 0,
  Longitude: 0
}

export interface IntelEntity {
  // Protocol is the IP protocol used to connect/communicate
  // the the described entity.
  Protocol: IPProtocol;
  // Port is the remote port number used.
  Port: number;
  // Domain is the domain name of the entity. This may either
  // be the domain name used in the DNS request or the
  // named returned from reverse PTR lookup.
  Domain: string;
  // CNAME is a list of CNAMEs that have been used
  // to resolve this entity.
  CNAME: string[] | null;
  // IP is the IP address of the entity.
  IP: string;
  // IPScope holds the classification of the IP address.
  IPScope: IPScope;
  // Country holds the country of residence of the IP address.
  Country: string;
  // ASN holds the number of the autonoumous system that operates
  // the IP.
  ASN: number;
  // ASOrg holds the AS owner name.
  ASOrg: string;
  // Coordinates contains the geographic coordinates of the entity.
  Coordinates: GeoCoordinates | null;
  // BlockedByLists holds a list of filter list IDs that
  // would have blocked the entity.
  BlockedByLists: string[] | null;
  // BlockedEntities holds a list of entities that have been
  // blocked by filter lists. Those entities can be ASNs, domains,
  // CNAMEs, IPs or Countries.
  BlockedEntities: string[] | null;
  // ListOccurences maps the blocked entity (see BlockedEntities)
  // to a list of filter-list IDs that contains it.
  ListOccurences: { [key: string]: string[] } | null;
}

export enum ScopeIdentifier {
  IncomingHost = "IH",
  IncomingLAN = "IL",
  IncomingInternet = "II",
  IncomingInvalid = "IX",
  PeerHost = "PH",
  PeerLAN = "PL",
  PeerInternet = "PI",
  PeerInvalid = "PX"
}

export const ScopeTranslation: { [key: string]: string } = {
  [ScopeIdentifier.IncomingHost]: "Device-Local Incoming",
  [ScopeIdentifier.IncomingLAN]: "LAN Incoming",
  [ScopeIdentifier.IncomingInternet]: "Internet Incoming",
  [ScopeIdentifier.PeerHost]: "Device-Local Outgoing",
  [ScopeIdentifier.PeerLAN]: "LAN Peer-to-Peer",
  [ScopeIdentifier.PeerInternet]: "Internet Peer-to-Peer",
  [ScopeIdentifier.IncomingInvalid]: "N/A",
  [ScopeIdentifier.PeerInvalid]: "N/A",
}

export interface ProcessContext {
  BinaryPath: string;
  ProcessName: string;
  ProfileName: string;
  PID: number;
  Profile: string;
  Source: string
}

// Reason justifies the decision on a connection
// verdict.
export interface Reason {
  // Msg holds a human readable message of the reason.
  Msg: string;
  // OptionKey, if available, holds the key of the
  // configuration option that caused the verdict.
  OptionKey: string;
  // Profile holds the profile the option setting has
  // been configured in.
  Profile: string;
  // Context may holds additional data about the reason.
  Context: any;
}

export enum ConnectionType {
  Undefined = 0,
  IPConnection = 1,
  DNSRequest = 2
}

export function IsDNSRequest(t: ConnectionType): t is ConnectionType.DNSRequest {
  return t === ConnectionType.DNSRequest;
}

export function IsIPConnection(t: ConnectionType): t is ConnectionType.IPConnection {
  return t === ConnectionType.IPConnection;
}

export interface DNSContext {
  Domain: string;
  ServedFromCache: boolean;
  RequestingNew: boolean;
  IsBackup: boolean;
  Filtered: boolean;
  FilteredEntries: string[], // RR
  Question: 'A' | 'AAAA' | 'MX' | 'TXT' | 'SOA' | 'SRV' | 'PTR' | 'NS' | string;
  RCode: 'NOERROR' | 'SERVFAIL' | 'NXDOMAIN' | 'REFUSED' | string;
  Modified: string;
  Expires: string;
}

export interface TunnelContext {
  Path: TunnelNode[];
  PathCost: number;
  RoutingAlg: 'default';
}

export interface GeoIPInfo {
  IP: string;
  Country: string;
  ASN: number;
  ASOwner: string;
}

export interface TunnelNode {
  ID: string;
  Name: string;
  IPv4?: GeoIPInfo;
  IPv6?: GeoIPInfo;

}

export interface CertInfo<dateType extends string | Date = string> {
  Subject: string;
  Issuer: string;
  AlternateNames: string[];
  NotBefore: dateType;
  NotAfter: dateType;
}

export interface TLSContext {
  Version: string;
  VersionRaw: number;
  SNI: string;
  Chain: CertInfo[][];
}

export interface Connection extends Record {
  // ID is a unique ID for the connection.
  ID: string;
  // Type defines the connection type.
  Type: ConnectionType;
  // TLS may holds additional data for the TLS
  // session.
  TLS: TLSContext | null;
  // DNSContext holds additional data about the DNS request for
  // this connection.
  DNSContext: DNSContext | null;
  // TunnelContext holds additional data about the SPN tunnel used for
  // the connection.
  TunnelContext: TunnelContext | null;
  // Scope defines the scope of the connection. It's an somewhat
  // weired field that may contain a ScopeIdentifier or a string.
  // In case of a string it may eventually be interpreted as a
  // domain name.
  Scope: ScopeIdentifier | string;
  // IPVersion is the version of the IP protocol used.
  IPVersion: IPVersion;
  // Inbound is true if the connection is incoming to
  // hte local system.
  Inbound: boolean;
  // IPProtocol is the protocol used by the connection.
  IPProtocol: IPProtocol;
  // LocalIP is the local IP address that is involved into
  // the connection.
  LocalIP: string;
  // LocalIPScope holds the classification of the local IP
  // address;
  LocalIPScope: IPScope;
  // LocalPort is the local port that is involved into the
  // connection.
  LocalPort: number;
  // Entity describes the remote entity that is part of the
  // connection.
  Entity: IntelEntity;
  // Verdict defines the final verdict.
  Verdict: Verdict;
  // Reason is the reason justifying the verdict of the connection.
  Reason: Reason;
  // Started holds the number of seconds in UNIX epoch time at which
  // the connection was initiated.
  Started: number;
  // End dholds the number of seconds in UNIX epoch time at which
  // the connection was considered terminated.
  Ended: number;
  // Tunneled is set to true if the connection was tunneled through the
  // SPN.
  Tunneled: boolean;
  // VerdictPermanent is set to true if the connection was marked and
  // handed back to the operating system.
  VerdictPermanent: boolean;
  // Inspecting is set to true if the connection is being inspected.
  Inspecting: boolean;
  // Encrypted is set to true if the connection is estimated as being
  // encrypted. Interpreting this field must be done with care!
  Encrypted: boolean;
  // Internal is set to true if this connection is done by the Portmaster
  // or any associated helper processes/binaries itself.
  Internal: boolean;
  // ProcessContext holds additional information about the process
  // that initated the connection.
  ProcessContext: ProcessContext;
  // ProfileRevisionCounter is used to track changes to the process
  // profile.
  ProfileRevisionCounter: number;
}

export interface ReasonContext {
  [key: string]: any;
}
