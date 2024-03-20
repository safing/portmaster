import { Directive, ElementRef, HostBinding, Input, isDevMode } from "@angular/core";
import { SfngTipUpPlacement } from "./utils";

@Directive({
  selector: '[sfngTipUpAnchor]',
})
export class SfngTipUpAnchorDirective implements SfngTipUpPlacement {
  constructor(
    public readonly elementRef: ElementRef,
  ) { }

  origin: 'left' | 'right' = 'right';
  offset: number = 10;

  @HostBinding('class.active-tipup-anchor')
  isActiveAnchor = false;

  @Input()
  set sfngTipUpAnchor(posSpec: string | undefined) {
    const parts = (posSpec || '').split(';')
    if (parts.length > 2) {
      if (isDevMode()) {
        throw new Error(`Invalid value "${posSpec}" for [sfngTipUpAnchor]`);
      }
      return;
    }

    if (parts[0] === 'left') {
      this.origin = 'left';
    } else {
      this.origin = 'right';
    }

    if (parts.length === 2) {
      this.offset = +parts[1];
      if (isNaN(this.offset)) {
        this.offset = 10;
      }
    } else {
      this.offset = 10;
    }
  }
}
