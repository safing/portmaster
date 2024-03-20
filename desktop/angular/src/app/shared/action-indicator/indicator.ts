import { animate, state, style, transition, trigger } from '@angular/animations';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, HostBinding, HostListener, Inject, OnInit } from '@angular/core';
import { takeUntil } from 'rxjs/operators';
import { ActionIndicatorRef, ACTION_REF } from './action-indicator.service';

@Component({
  templateUrl: './indicator.html',
  styleUrls: ['./indicator.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [
    trigger('slideIn', [
      state('void', style({
        opacity: 0,
        transform: 'translateY(32px)'
      })),

      state('showing', style({
        opacity: 1,
        transform: 'translateY(0px)'
      })),

      state('replace', style({
        transform: 'translateY(0px) rotate(-3deg)',
        zIndex: -100,
      })),

      transition('showing => replace', animate('10ms cubic-bezier(0, 0, 0.2, 1)')),
      transition('void => *', animate('220ms cubic-bezier(0, 0, 0.2, 1)')),

      transition('showing => void', animate('220ms cubic-bezier(0, 0, 0.2, 1)', style({
        opacity: 0,
        transform: 'translateX(-100%)'
      }))),

      transition('replace => void', animate('220ms cubic-bezier(0, 0, 0.2, 1)', style({
        opacity: 0,
        transform: 'translateY(-64px) rotate(-3deg)'
      })))
    ])
  ]
})
export class IndicatorComponent implements OnInit {
  constructor(
    @Inject(ACTION_REF)
    public ref: ActionIndicatorRef,
    public cdr: ChangeDetectorRef,
  ) { }

  @HostBinding('@slideIn')
  state = 'showing';

  @HostBinding('class.error')
  isError = this.ref.status === 'error';

  @HostListener('click')
  closeIndicator() {
    this.ref.close();
  }

  @HostListener('@slideIn.done', ['$event'])
  onAnimationDone() {
    if (this.state === 'replace') {
      this.ref.close();
    }
  }

  ngOnInit() {
    this.ref.onCloseReplace
      .pipe(
        takeUntil(this.ref.onClose),
      )
      .subscribe(state => {
        this.state = 'replace';
        this.cdr.detectChanges();
      })
  }
}

