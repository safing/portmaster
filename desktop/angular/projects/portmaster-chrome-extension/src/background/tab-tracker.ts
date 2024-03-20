import { deepClone } from "@safing/portmaster-api";

export interface Request {
  /** The ID assigned by the browser */
  id: string;

  /** The domain this request was for */
  domain: string;

  /** The timestamp in milliseconds since epoch at which the request was initiated */
  time: number;

  /** Whether or not this request errored with net::ERR_ADDRESS_UNREACHABLE */
  isUnreachable: boolean;
}

/**
 * TabTracker tracks requests to domains made by a single browser tab.
 */
export class TabTracker {
  /** A list of requests observed for this tab order by time they have been initiated */
  private requests: Request[] = [];

  /** A lookup map for requests to specific domains */
  private byDomain = new Map<string, Request[]>();

  /** A lookup map for requests by the chrome request ID */
  private byRequestId = new Map<string, Request>;

  constructor(public readonly tabId: number) { }

  /** Returns an array of all requests observed in this tab. */
  allRequests(): Request[] {
    return deepClone(this.requests)
  }

  /** Returns a list of requests that have been observed for domain */
  forDomain(domain: string): Request[] {
    if (!domain.endsWith(".")) {
      domain += "."
    }

    return this.byDomain.get(domain) || [];
  }

  /** Call to add the details of a web-request to this tab-tracker */
  trackRequest(details: chrome.webRequest.WebRequestDetails) {
    // If this is the wrong tab ID ignore the request details
    if (details.tabId !== this.tabId) {
      console.error(`TabTracker.trackRequest: called with wrong tab ID. Expected ${this.tabId} but got ${details.tabId}`)

      return;
    }

    // if the type of the request is for the main_frame the user switched to a new website.
    // In that case, we can wipe out all currently stored requests as the user will likely not
    // care anymore.
    if (details.type === "main_frame") {
      this.clearState();
    }

    // get the domain of the request normalized to contain the trailing dot.
    let domain = new URL(details.url).host;
    if (!domain.endsWith(".")) {
      domain += "."
    }

    const req: Request = {
      id: details.requestId,
      domain: domain,
      time: details.timeStamp,
      isUnreachable: false, // we don't actually know that yet
    }

    this.requests.push(req);
    this.byRequestId.set(req.id, req)

    // Add the request to the by-domain lookup map
    let byDomainRequests = this.byDomain.get(req.domain);
    if (!byDomainRequests) {
      byDomainRequests = [];
      this.byDomain.set(req.domain, byDomainRequests)
    }
    byDomainRequests.push(req)

    console.log(`DEBUG: observed request ${req.id} to ${req.domain}`)
  }

  /** Call to notify the tab-tracker of a request error */
  trackError(errorDetails: chrome.webRequest.WebResponseErrorDetails) {
    // we only care about net::ERR_ADDRESS_UNREACHABLE here because that's how the
    // Portmaster blocks the request.

    // TODO(ppacher): docs say we must not rely on that value so we should figure out a better
    // way to detect if the error is caused by the Portmaster.
    if (errorDetails.error !== "net::ERR_ADDRESS_UNREACHABLE") {
      return;
    }

    // the the previsouly observed request by the request ID.
    const req = this.byRequestId.get(errorDetails.requestId)
    if (!req) {
      console.error("TabTracker.trackError: request has not been observed before")

      return
    }

    // make sure the error details actually happend for the observed tab.
    if (errorDetails.tabId !== this.tabId) {
      console.error(`TabTracker.trackRequest: called with wrong tab ID. Expected ${this.tabId} but got ${errorDetails.tabId}`)

      return;
    }

    // mark the request as unreachable.
    req.isUnreachable = true;
    console.log(`DEBUG: marked request ${req.id} to ${req.domain} as unreachable`)
  }

  /** Clears the current state of the tab tracker */
  private clearState() {
    this.requests = [];
    this.byDomain = new Map();
    this.byRequestId = new Map();
  }
}
