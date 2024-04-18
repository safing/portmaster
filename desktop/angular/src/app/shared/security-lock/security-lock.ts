import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, Input, OnInit, inject } from "@angular/core";
import { SecurityLevel } from "@safing/portmaster-api";
import { combineLatest } from "rxjs";
import { FailureStatus, StatusService, Subsystem } from "src/app/services";
import { fadeInAnimation, fadeOutAnimation } from "../animations";

interface SecurityOption {
  level: SecurityLevel;
  displayText: string;
  class: string;
  subText?: string;
}

@Component({
  selector: 'app-security-lock',
  templateUrl: './security-lock.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrls: ['./security-lock.scss'],
  animations: [
    fadeInAnimation,
    fadeOutAnimation
  ]
})
export class SecurityLockComponent implements OnInit {
  private destroyRef = inject(DestroyRef);

  lockLevel: SecurityOption | null = null;

  /** The display mode for the security lock */
  @Input()
  mode: 'small' | 'full' = 'full'

  constructor(
    private statusService: StatusService,
    private cdr: ChangeDetectorRef,
  ) { }

  ngOnInit(): void {
    combineLatest([
      this.statusService.status$,
      this.statusService.watchSubsystems()
    ])
      .subscribe(([status, subsystems]) => {
        const activeLevel = status.ActiveSecurityLevel;
        const suggestedLevel = status.ThreatMitigationLevel;

        // By default the lock is green and we are "Secure"
        this.lockLevel = {
          level: SecurityLevel.Normal,
          class: 'text-green-300',
          displayText: 'Secure',
        }

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

        // update the failure level depending on the  highest
        // failure status.
        switch (failureStatus) {
          case FailureStatus.Warning:
            this.lockLevel = {
              level: SecurityLevel.High,
              class: 'text-yellow-300',
              displayText: 'Warning'
            }
            break;
          case FailureStatus.Error:
            this.lockLevel = {
              level: SecurityLevel.Extreme,
              class: 'text-red-300',
              displayText: 'Insecure'
            }
            break;
        }

        // if the auto-pilot would suggest a higher (mitigation) level
        // we are always Insecure
        if (activeLevel < suggestedLevel) {
          this.lockLevel = {
            level: SecurityLevel.High,
            class: 'high',
            displayText: 'Insecure'
          }
        }

        this.cdr.markForCheck();
      });
  }
}
