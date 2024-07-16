import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, Input, OnInit, inject } from "@angular/core";
import { SecurityLevel } from "@safing/portmaster-api";
import { combineLatest } from "rxjs";
import { StatusService, ModuleStateType } from "src/app/services";
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
          displayText: 'Secure',
        }

        // update the shield depending on the worst state.
        switch (status.WorstState.Type) {
          case ModuleStateType.Warning:
            this.lockLevel = {
              level: SecurityLevel.High,
              class: 'text-yellow-300',
              displayText: 'Warning'
            }
            break;
          case ModuleStateType.Error:
            this.lockLevel = {
              level: SecurityLevel.Extreme,
              class: 'text-red-300',
              displayText: 'Insecure'
            }
            break;
        }

        this.cdr.markForCheck();
      });
  }
}
