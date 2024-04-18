import { ComponentPortal } from "@angular/cdk/portal";
import { HttpErrorResponse } from "@angular/common/http";
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, ComponentRef, EventEmitter, OnInit, inject } from "@angular/core";
import { SFNG_DIALOG_REF, SfngDialogRef, SfngDialogService } from "@safing/ui";
import { Observable, map, mergeMap, of } from "rxjs";
import { INTEGRATION_SERVICE } from "src/app/integration";
import { SupportHubService, SupportSection } from "src/app/services";
import { ActionIndicatorService } from "src/app/shared/action-indicator";

export interface TicketData {
  debugInfo: string;
  repo: string;
  title: string;
  sections: SupportSection[];
}

export interface GithubIssue extends TicketData {
  type: 'github',
  generateUrl?: boolean;
  preset?: string;
}

export interface PrivateTicket extends TicketData {
  type: 'private',
  email?: string,
}

export type TicketInfo = GithubIssue | PrivateTicket;


@Component({
  templateUrl: './progress-dialog.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [
    `
    :host {
      @apply block flex flex-col gap-8 relative;
    }
    `,
  ]
})
export class SupportProgressDialogComponent implements OnInit {

  /** Static method to open the support-progress dialog. */
  static open(dialog: SfngDialogService, data: TicketInfo): Observable<void> {
    const ref = dialog.create(SupportProgressDialogComponent, {
      data,
      dragable: true,
      backdrop: false,
      autoclose: false,
    });

    return (ref.contentRef() as ComponentRef<SupportProgressDialogComponent>)
      .instance
      .done;
  }


  private readonly cdr = inject(ChangeDetectorRef);
  private readonly supporthub = inject(SupportHubService);
  private readonly uai = inject(ActionIndicatorService);

  readonly integration = inject(INTEGRATION_SERVICE);

  readonly dialogRef: SfngDialogRef<this, any, TicketInfo> = inject(SFNG_DIALOG_REF);

  /** Holds the current state of the issue-creation */
  state: '' | 'debug-info' | 'create-issue' | 'create-ticket' | 'done' | 'error' = '';

  /** The URL to the github issue once it was created. */
  url: string = '';

  /** The error message if one occured */
  error: string = '';

  /** Emits once the issue has been created successfully */
  done = new EventEmitter<void>;

  ngOnInit(): void {
    this.createSupportRequest();
  }

  setState(state: typeof this['state']) {
    this.state = state;
    this.cdr.detectChanges();
  }

  createSupportRequest(): void {
    const data = this.dialogRef.data;
    let stream = of('')

    // Upload debug info
    if (data.debugInfo) {
      stream = new Observable((observer) => {
        this.state = 'debug-info';
        this.cdr.detectChanges();

        this.supporthub.uploadText('debug-info', data.debugInfo)
          .subscribe(observer);
      })
    }

    // either create on github or create a private ticket through support-hub
    if (data.type === 'github') {
      stream = stream.pipe(
        mergeMap((url) => {
          this.state = 'create-issue';
          this.cdr.detectChanges();

          return this.supporthub.createIssue(
            data.repo,
            data.preset || '',
            data.title,
            data.sections,
            url,
            {
              generateUrl: data.generateUrl || false
            },
          );
        })
      )
    } else {
      stream = stream.pipe(
        mergeMap((url) => {
          this.state = 'create-ticket';
          this.cdr.markForCheck();

          return this.supporthub.createTicket(
            data.repo,
            data.title,
            data.email || '',
            data.sections,
            url
          )
        }),
        map(() => '')
      )
    }

    stream.subscribe({
      next: (url) => {
        this.state = 'done';
        this.url = url;
        this.cdr.markForCheck();

        this.done.next();
      },

      error: (err) => {
        console.error("error", err);

        this.state = 'error';
        if (err instanceof HttpErrorResponse && err.error instanceof ProgressEvent) {
          this.error = err.statusText;
        } else {
          this.error = this.uai.getErrorMessage(err);
        }

        this.cdr.markForCheck();
      }
    });
  }

  copyUrl() {
    if (!this.url) {
      return
    }

    this.integration.writeToClipboard(this.url)
      .then(() => this.uai.success('URL Copied To Clipboard'))
      .catch(err => this.uai.error('Failed to Copy To Clipboard', this.uai.getErrorMessage(err)))
  }
}
