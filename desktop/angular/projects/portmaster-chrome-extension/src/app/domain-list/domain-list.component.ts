import { ChangeDetectionStrategy, ChangeDetectorRef, Component, OnInit } from "@angular/core";
import { Netquery, NetqueryConnection } from "@safing/portmaster-api";
import { ListRequests, NotifyRequests } from "../../background/commands";
import { Request } from '../../background/tab-tracker';

interface DomainRequests {
  domain: string;
  requests: Request[];
  latestIsBlocked: boolean;
  lastConn?: NetqueryConnection;
}

@Component({
  selector: 'ext-domain-list',
  templateUrl: './domain-list.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [
    `
    :host {
      @apply flex flex-grow flex-col overflow-auto;
    }
    `
  ]
})
export class ExtDomainListComponent implements OnInit {
  requests: DomainRequests[] = [];

  constructor(
    private netquery: Netquery,
    private cdr: ChangeDetectorRef
  ) { }

  ngOnInit() {
    // setup listening for requests sent from our background script
    const self = this;
    chrome.runtime.onMessage.addListener((msg: NotifyRequests) => {
      if (typeof msg !== 'object') {
        console.error('Received invalid message from background script')

        return;
      }

      console.log(`DEBUG: received command ${msg.type} from background script`)

      switch (msg.type) {
        case 'notifyRequests':
          self.updateRequests(msg.requests);
          break;

        default:
          console.error('Received unknown command from background script')
      }
    })

    this.loadRequests();
  }

  updateRequests(req: Request[]) {
    let m = new Map<string, DomainRequests>();

    this.requests.forEach(obj => {
      obj.requests = [];
      m.set(obj.domain, obj);
    });

    req.forEach(r => {
      let obj = m.get(r.domain);
      if (!obj) {
        obj = {
          domain: r.domain,
          requests: [],
          latestIsBlocked: false
        }
        m.set(r.domain, obj)
      }

      obj.requests.push(r);
    })

    this.requests = [];
    Array.from(m.keys()).sort()
      .map(key => m.get(key)!)
      .forEach(obj => {
        this.requests.push(obj)

        this.netquery.query({
          query: {
            domain: obj.domain,
          },
          orderBy: [
            {
              field: 'started',
              desc: true,
            }
          ],
          page: 0,
          pageSize: 1,
        })
          .subscribe(result => {
            if (!result[0]) {
              return;
            }

            obj.latestIsBlocked = !result[0].allowed;
            obj.lastConn = result[0] as NetqueryConnection;
          })
      })

    this.cdr.detectChanges();
  }

  private loadRequests() {
    const cmd: ListRequests = {
      type: 'listRequests',
      tabId: 'current'
    }

    const self = this;
    chrome.runtime.sendMessage(cmd, (response: any) => {
      if (Array.isArray(response)) {
        self.updateRequests(response)

        return;
      }

      console.error(response);
    })
  }
}
