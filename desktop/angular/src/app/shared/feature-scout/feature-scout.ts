import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, OnInit, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { BoolSetting, ConfigService, FeatureID, Netquery, SPNService, SPNStatus, UserProfile } from "@safing/portmaster-api";
import { catchError, of } from "rxjs";
import { fadeInAnimation, fadeOutAnimation } from "../animations";
import { CountryFlagModule } from 'src/app/shared/country-flag';

@Component({
  selector: 'app-feature-scout',
  templateUrl: './feature-scout.html',
  styleUrls: [
    './feature-scout.scss'
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [
    fadeInAnimation,
    fadeOutAnimation,
  ]
})
export class FeatureScoutComponent implements OnInit {
  private destroyRef = inject(DestroyRef);

  /** The current SPN user profile */
  profile: UserProfile | null = null;

  /** Whether or not the SPN is currently enabled */
  spnEnabled = false;

  /** The current status of the SPN module */
  spnStatus: SPNStatus | null = null;

  /** Whether or not the Network History is currently enabled */
  historyEnabled = false;

  /** Returns whether or not the current package has the SPN feature */
  get packageHasSPN() {
    return this.profile?.current_plan?.feature_ids?.includes(FeatureID.SPN)
  }

  /** Returns whether or not the current package has the Network History feature */
  get packageHasHistory() {
    return this.profile?.current_plan?.feature_ids?.includes(FeatureID.History)
  }

  constructor(
    private configService: ConfigService,
    private spnService: SPNService,
    private cdr: ChangeDetectorRef,
  ) { }

  ngOnInit(): void {
    this.spnService
      .profile$
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        catchError(() => of(null))
      )
      .subscribe(profile => {
        this.profile = profile || null;

        this.cdr.markForCheck();
      });

    this.spnService.status$
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(status => {
        this.spnStatus = status;

        this.cdr.markForCheck();
      })

    this.configService.watch<BoolSetting>("spn/enable")
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(value => {
        this.spnEnabled = value;

        this.cdr.markForCheck();
      });

    this.configService.watch<BoolSetting>("history/enable")
    .pipe(takeUntilDestroyed(this.destroyRef))
    .subscribe(value => {
      this.historyEnabled = value;

      this.cdr.markForCheck();
    });
  }

  setSPNEnabled(v: boolean) {
    this.configService.save(`spn/enable`, v)
      .subscribe();
  }

  setHistoryEnabled(v: boolean) {
    this.configService.save(`history/enable`, v)
      .subscribe();
  }
}
