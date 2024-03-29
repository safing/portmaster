import { ChangeDetectionStrategy, ChangeDetectorRef, Component, OnInit } from '@angular/core';
import { ConfigService, SecurityLevel } from '@safing/portmaster-api';
import { combineLatest } from 'rxjs';
import { FailureStatus, StatusService, Subsystem } from 'src/app/services';

interface SecurityOption {
  level: SecurityLevel;
  displayText: string;
  class: string;
  subText?: string;
}

@Component({
  selector: 'app-status-pilot',
  templateUrl: './pilot-widget.html',
  styleUrls: [
    './pilot-widget.scss'
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class StatusPilotComponent implements OnInit {
  activeLevel: SecurityLevel = SecurityLevel.Off;
  selectedLevel: SecurityLevel = SecurityLevel.Off;
  suggestedLevel: SecurityLevel = SecurityLevel.Off;
  activeOption: SecurityOption | null = null;
  selectedOption: SecurityOption | null = null;

  mode: 'auto' | 'manual' = 'auto';

  get activeLevelText() {
    return this.options.find(opt => opt.level === this.activeLevel)?.displayText || '';
  }

  readonly options: SecurityOption[] = [
    {
      level: SecurityLevel.Normal,
      displayText: 'Trusted',
      class: 'low',
      subText: 'Home Network'
    },
    {
      level: SecurityLevel.High,
      displayText: 'Untrusted',
      class: 'medium',
      subText: 'Public Network'
    },
    {
      level: SecurityLevel.Extreme,
      displayText: 'Danger',
      class: 'high',
      subText: 'Hacked Network'
    },
  ];

  get networkRatingEnabled$() { return this.configService.networkRatingEnabled$ }

  constructor(
    private statusService: StatusService,
    private changeDetectorRef: ChangeDetectorRef,
    private configService: ConfigService,
  ) { }

  ngOnInit() {

    combineLatest([
      this.statusService.status$,
      this.statusService.watchSubsystems()
    ])
      .subscribe(([status, subsystems]) => {
        this.activeLevel = status.ActiveSecurityLevel;
        this.selectedLevel = status.SelectedSecurityLevel;
        this.suggestedLevel = status.ThreatMitigationLevel;

        if (this.selectedLevel === SecurityLevel.Off) {
          this.mode = 'auto';
        } else {
          this.mode = 'manual';
        }

        this.selectedOption = this.options.find(opt => opt.level === this.selectedLevel) || null;
        this.activeOption = this.options.find(opt => opt.level === this.activeLevel) || null;

        // Find the highest failure-status reported by any module
        // of any subsystem.
        const failureStatus = subsystems.reduce((value: FailureStatus, system: Subsystem) => {
          if (system.FailureStatus != 0) {
            console.log(system);
          }
          return system.FailureStatus > value
            ? system.FailureStatus
            : value;
        }, FailureStatus.Operational)

        this.changeDetectorRef.markForCheck();
      });
  }

  updateMode(mode: 'auto' | 'manual') {
    this.mode = mode;

    if (mode === 'auto') {
      this.selectLevel(SecurityLevel.Off);
    } else {
      this.selectLevel(this.activeLevel);
    }
  }

  selectLevel(level: SecurityLevel) {
    if (this.mode === 'auto' && level !== SecurityLevel.Off) {
      this.mode = 'manual';
    }

    this.statusService.selectLevel(level).subscribe();
  }
}
