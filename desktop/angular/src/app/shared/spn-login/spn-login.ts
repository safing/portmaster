import { coerceBooleanProperty } from "@angular/cdk/coercion";
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, Input, OnInit, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { SPNService, UserProfile } from "@safing/portmaster-api";
import { catchError, finalize, of } from "rxjs";
import { ActionIndicatorService } from "../action-indicator";

@Component({
  selector: 'app-spn-login',
  templateUrl: './spn-login.html',
  styleUrls: ['./spn-login.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class SPNLoginComponent implements OnInit {
  private destroyRef = inject(DestroyRef);

  /** The current user profile if the user is already logged in */
  profile: UserProfile | null = null;

  /** The value of the username text box */
  username: string = '';

  /** The value of the password text box */
  password: string = '';

  @Input()
  set forcedLogout(v: any) {
    this._forcedLogout = coerceBooleanProperty(v);
  }
  get forcedLogout() { return this._forcedLogout }
  private _forcedLogout = false;

  constructor(
    private spnService: SPNService,
    private uai: ActionIndicatorService,
    private cdr: ChangeDetectorRef
  ) { }

  login(): void {
    if (!this.username || !this.password) {
      return;
    }

    this.spnService.login({
      username: this.username,
      password: this.password
    })
      .pipe(finalize(() => {
        this.password = '';
      }))
      .subscribe(this.uai.httpObserver('SPN Login', 'SPN Login'))
  }

  ngOnInit(): void {
    this.spnService.profile$
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        catchError(() => of(null))
      )
      .subscribe(profile => {
        this.profile = profile || null;

        if (!!this.profile) {
          this.username = this.profile.username;
        }

        this.cdr.markForCheck();
      });
  }
}
