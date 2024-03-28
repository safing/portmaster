import { coerceBooleanProperty } from "@angular/cdk/coercion";
import { Directive, ElementRef, Input, OnInit } from "@angular/core";

@Directive({
  // eslint-disable-next-line @angular-eslint/directive-selector
  selector: '[autoFocus]',
})
export class AutoFocusDirective implements OnInit {
  private _focus = true;
  private _afterInit = false;

  @Input('autoFocus')
  set focus(v: any) {
    this._focus = coerceBooleanProperty(v) !== false;

    if (this._afterInit && this.elementRef) {
      this.elementRef.nativeElement.focus()
    }
  }

  constructor(private elementRef: ElementRef) { }

  ngOnInit(): void {
    setTimeout(() => {
      if (this._focus) {
        this.elementRef.nativeElement.focus();
      }
    }, 100)

    this._afterInit = true;
  }
}
