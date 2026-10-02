import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, Input, OnInit, inject } from "@angular/core";
import { SecurityLevel } from "@safing/portmaster-api";
import { combineLatest } from "rxjs";
import { StatusService, ModuleStateType, GetModuleState, ControlPauseStateData } from "src/app/services";
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
      this.statusService.status$.subscribe(status => {
        // By default the lock is green and we are "Secure"
        this.lockLevel = {
          level: SecurityLevel.Normal,
          class: 'text-green-300',
          displayText: '安全',
        }

        // update the shield depending on the worst state.
        switch (status.WorstState.Type) {
          case ModuleStateType.Warning:
            this.lockLevel = {
              level: SecurityLevel.High,
              class: 'text-yellow-300',
              displayText: '警告'
            }
            break;
          case ModuleStateType.Error:
            this.lockLevel = {
              level: SecurityLevel.Extreme,
              class: 'text-red-300',
              displayText: '不安全'
            }
            break;
        }

        // Checking for Control:Paused state
        const pausedState = GetModuleState(status, 'Control', 'control:paused');
        if (pausedState?.Data) {
          const pauseData = pausedState.Data as ControlPauseStateData;
          if (pauseData.Interception === true) {
            this.lockLevel.displayText = '不安全：已暂停';
          } else if (pauseData.SPN === true) {
            this.lockLevel.displayText = '安全（SPN 已暂停）';
          }
        }

        this.cdr.markForCheck();
      });
  }
}
