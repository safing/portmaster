import { debounceTime, Subject } from "rxjs";
import { CallRequest, ListRequests, NotifyRequests } from "./background/commands";
import { Request, TabTracker } from "./background/tab-tracker";
import { getCurrentTab } from "./background/tab-utils";

export class BackgroundService {
  /** a lookup map for tab trackers by tab-id */
  private trackers = new Map<number, TabTracker>();

  /** used to signal the pop-up that new requests arrived */
  private notifyRequests = new Subject<void>();

  constructor() {
    // register a navigation-completed listener. This is fired when the user switches to a new website
    // by entering it in the browser address bar.
    chrome.webNavigation.onCompleted.addListener((details) => {
      console.log("event: webNavigation.onCompleted", details);
    })

    // request event listeners for new requests and errors that occured for them.
    // We only care about http and https here.
    const filter = {
      urls: [
        'http://*/*',
        'https://*/*'
      ]
    }
    chrome.webRequest.onBeforeRequest.addListener(details => this.handleOnBeforeRequest(details), filter)
    chrome.webRequest.onErrorOccurred.addListener(details => this.handleOnErrorOccured(details), filter)

    // make sure we can communicate with the extension popup
    chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => this.handleMessage(msg, sender, sendResponse))

    // set-up signalling of new requests to the pop-up
    this.notifyRequests
      .pipe(debounceTime(500))
      .subscribe(async () => {
        const currentTab = await getCurrentTab();
        if (!!currentTab && !!currentTab.id) {
          const msg: NotifyRequests = {
            type: 'notifyRequests',
            requests: this.mustGetTab({ tabId: currentTab.id }).allRequests()
          }

          chrome.runtime.sendMessage(msg)
        }
      })
  }

  /** Callback for messages sent by the popup */
  private handleMessage(msg: CallRequest, sender: chrome.runtime.MessageSender, sendResponse: (msg: any) => void) {
    console.log(`DEBUG: got message from ${sender.origin} (tab=${sender.tab?.id})`)

    if (typeof msg !== 'object') {
      console.error(`Received invalid message from popup`, msg)

      return;
    }

    let response: Promise<any>;
    switch (msg.type) {
      case 'listRequests':
        response = this.handleListRequests(msg)
        break;

      default:
        response = Promise.reject("unknown command")
    }

    response
      .then(res => {
        console.log(`DEBUG: sending response for command ${msg.type}`, res)
        sendResponse(res);
      })
      .catch(err => {
        console.error(`Failed to handle command ${msg.type}`, err)
        sendResponse({
          type: 'error',
          details: err
        });
      })
  }

  /** Returns a list of all observed requests based on the filter in msg. */
  private async handleListRequests(msg: ListRequests): Promise<Request[]> {
    if (msg.tabId === 'current') {
      const currentID = (await getCurrentTab()).id
      if (!currentID) {
        return [];
      }

      msg.tabId = currentID;
    }

    const tracker = this.mustGetTab({ tabId: msg.tabId as number })

    if (!!msg.domain) {
      return tracker.forDomain(msg.domain)
    }

    return tracker.allRequests()
  }

  /** Callback for chrome.webRequest.onBeforeRequest */
  private handleOnBeforeRequest(details: chrome.webRequest.WebRequestDetails) {
    this.mustGetTab(details).trackRequest(details)

    this.notifyRequests.next();
  }

  /** Callback for chrome.webRequest.onErrorOccured */
  private handleOnErrorOccured(details: chrome.webRequest.WebResponseErrorDetails) {
    this.mustGetTab(details).trackError(details);

    this.notifyRequests.next();
  }

  /** Returns the tab-tracker for tabId. Creates a new tracker if none exists. */
  private mustGetTab({ tabId }: { tabId: number }): TabTracker {
    let tracker = this.trackers.get(tabId);
    if (!tracker) {
      tracker = new TabTracker(tabId)
      this.trackers.set(tabId, tracker)
    }

    return tracker;
  }
}

/** start the background service once we got successfully installed. */
chrome.runtime.onInstalled.addListener(() => {
  new BackgroundService()
});
