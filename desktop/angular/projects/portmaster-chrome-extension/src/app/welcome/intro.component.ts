import { Component } from "@angular/core";
import { Router } from "@angular/router";
import { MetaAPI } from "@safing/portmaster-api";
import { Subject, takeUntil } from "rxjs";

@Component({
  templateUrl: './intro.component.html',
  styles: [
    `
    :host {
      @apply flex flex-col h-full;
    }
    `
  ]
})
export class IntroComponent {
  private cancelRequest$ = new Subject<void>();

  state: 'authorizing' | 'failed' | '' = '';

  constructor(
    private meta: MetaAPI,
    private router: Router,
  ) { }

  authorizeExtension() {
    // cancel any pending request
    this.cancelRequest$.next();

    this.state = 'authorizing';
    this.meta.requestApplicationAccess("Portmaster Browser Extension")
      .pipe(takeUntil(this.cancelRequest$))
      .subscribe({
        next: token => {
          chrome.storage.local.set(token);
          console.log(token);
          this.router.navigate(['/'])
        },
        error: err => {
          this.state = 'failed';
        }
      })
  }
}
