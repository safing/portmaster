import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, Inject, OnInit, Optional, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { SPNService, UserProfile } from "@safing/portmaster-api";
import { SFNG_DIALOG_REF, SfngDialogRef } from "@safing/ui";
import { catchError, delay, of, tap } from "rxjs";
import { ActionIndicatorService } from "../action-indicator";

@Component({
  templateUrl: './spn-account-details.html',
  styleUrls: ['./spn-account-details.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SPNAccountDetailsComponent implements OnInit {
  private destroyRef = inject(DestroyRef);

  /** Whether or not we're currently refreshing the user profile from the customer agent */
  refreshing = false;

  /** Whether or not we're still waiting for the user profile to be fetched from the backend */
  loadingProfile = true;

  currentUser: UserProfile | null = null;

  constructor(
    private spnService: SPNService,
    private cdr: ChangeDetectorRef,
    private uai: ActionIndicatorService,
    @Inject(SFNG_DIALOG_REF) @Optional() public dialogRef: SfngDialogRef<any>,
  ) { }

  /**
   * Force a refresh of the local user account
   *
   * @private - template only
   */
  refreshAccount() {
    this.refreshing = true;
    this.spnService.userProfile(true)
      .pipe(
        delay(1000),
        tap(() => {
          this.refreshing = false;
          this.cdr.markForCheck();
        }),
      )
      .subscribe()
  }

  /**
   * Logout of your safing account
   *
   * @private - template only
   */
  logout() {
    this.spnService.logout()
      .pipe(tap(() => this.dialogRef?.close()))
      .subscribe(this.uai.httpObserver('SPN Logout', 'SPN Logout'))
  }

  ngOnInit(): void {
    this.loadingProfile = false;
    this.spnService.profile$
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        catchError(err => of(null)),
      )
      .subscribe({
        next: (profile) => {
          this.loadingProfile = false;
          this.currentUser = profile || null;

          this.cdr.markForCheck();
        },
        complete: () => {
          // Database entry deletion will complete the observer.
          this.loadingProfile = false;
          this.currentUser = null;

          this.cdr.markForCheck();
        },
      })
  }
}
