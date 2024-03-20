import { ChangeDetectionStrategy, ChangeDetectorRef, Component, OnInit, TrackByFunction, inject } from "@angular/core";
import { map } from "rxjs";
import { INTEGRATION_SERVICE } from "src/app/integration";
import { Issue, SupportHubService } from "src/app/services";

/** The name of the SPN repository used to filter SPN support hub issues. */
const SPNRepository = "spn";

/** A set of issue labels that are eligible to be displayed */
const SPNTagSet = new Set<string>(["network status"])

interface _Issue extends Issue {
  expanded: boolean;
}

@Component({
  selector: 'app-spn-network-status',
  templateUrl: './spn-network-status.html',
  styleUrls: ['./spn-network-status.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SPNNetworkStatusComponent implements OnInit {
  private readonly integration = inject(INTEGRATION_SERVICE);
  private readonly supportHub = inject(SupportHubService);
  private readonly cdr = inject(ChangeDetectorRef);

  /** trackIssue is used as a track-by function when rendering SPN issues. */
  trackIssue: TrackByFunction<_Issue> = (_: number, issue: _Issue) => issue.url;

  spnIssues: _Issue[] = [];

  ngOnInit(): void {
    this.supportHub.loadIssues()
      .pipe(
        map(issues => {
          return issues
            .filter(issue => issue.repository === SPNRepository && issue.labels?.some(l => {
              return SPNTagSet.has(l);
            }))
            .reverse()
        })
      )
      .subscribe(issues => {
        let spnIssues: _Issue[] = issues
          .map(i => {
            const existing = this.spnIssues.find(existing => existing.url === i.url);
            return {
              ...i,
              expanded: existing !== undefined ? existing.expanded : false
            }
          })
        this.spnIssues = spnIssues;
        this.cdr.markForCheck();
      })
  }

  /**
   * Open a github issue in a new tab/window
   *
   * @private - template only
   */
  openIssue(issue: Issue) {
    this.integration.openExternal(issue.url);
  }
}
