import { AfterViewInit, ChangeDetectionStrategy, ChangeDetectorRef, Component, HostListener, OnDestroy, QueryList, ViewChild, ViewChildren } from "@angular/core";
import { SfngTabComponent, SfngTabGroupComponent } from '@safing/ui';
import { filter, interval, startWith, Subscription } from 'rxjs';

@Component({
  // eslint-disable-next-line @angular-eslint/component-selector
  selector: 'spn-feature-carousel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './spn-feature-carousel.html',
  styleUrls: [
    './spn-feature-carousel.scss'
  ]
})
export class SPNFeatureCarouselComponent implements AfterViewInit, OnDestroy {
  private sub: Subscription = Subscription.EMPTY;

  pause = false;
  currentIndex = -1;

  @HostListener('mouseenter')
  onMouseEnter() {
    this.pause = true
  }

  @HostListener('mouseleave')
  onMouseLeave() {
    this.pause = false;
  }

  /** A list of all carousel templates */
  @ViewChildren(SfngTabComponent)
  carousel!: QueryList<SfngTabComponent>;

  @ViewChild(SfngTabGroupComponent)
  tabGroup!: SfngTabGroupComponent;

  constructor(
    private cdr: ChangeDetectorRef
  ) { }

  ngAfterViewInit(): void {
    this.sub = interval(5000)
      .pipe(
        startWith(-1),
        filter(() => !this.pause),
      )
      .subscribe(() => {
        this.openTab(this.currentIndex + 1, 'left')
      })
  }

  ngOnDestroy(): void {
    this.sub.unsubscribe()
  }

  openTab(idx: number, direction?: 'left' | 'right') {
    // force animation to circle if we go before the first
    // or after the last one.
    if (idx < 0) {
      idx = this.carousel.length - 1;
      direction = 'right'
    }
    if (idx >= this.carousel.length) {
      direction = 'left'
    }

    this.currentIndex = idx % this.carousel.length;
    this.tabGroup.activateTab(this.currentIndex, direction)!;
    this.cdr.markForCheck();
  }

  showNext() {
    this.sub.unsubscribe()

    this.openTab(this.currentIndex + 1)
  }

  showPrev() {
    this.sub.unsubscribe()

    this.openTab(this.currentIndex - 1)
  }
}
