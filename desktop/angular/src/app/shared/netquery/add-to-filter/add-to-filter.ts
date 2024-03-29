import { ChangeDetectorRef, Directive, HostBinding, HostListener, Input, OnDestroy, OnInit, inject } from "@angular/core";
import { NetqueryConnection } from "@safing/portmaster-api";
import { Subscription, combineLatest } from "rxjs";
import { ActionIndicatorService } from "../../action-indicator";
import { NetqueryHelper } from "../connection-helper.service";
import { INTEGRATION_SERVICE } from "src/app/integration";

@Directive({
  selector: '[sfngAddToFilter]'
})
export class SfngNetqueryAddToFilterDirective implements OnInit, OnDestroy {
  private subscription = Subscription.EMPTY;
  private readonly integration = inject(INTEGRATION_SERVICE);

  @Input('sfngAddToFilter')
  key: keyof NetqueryConnection | null = null;

  @Input('sfngAddToFilterValue')
  set value(v: any | any[]) {
    if (!Array.isArray(v)) {
      v = [v]
    }
    this._values = v;
  }
  private _values: any[] = [];

  @HostListener('click', ['$event'])
  onClick(evt: MouseEvent) {
    if (!this.key) {
      return
    }

    let prevent = false
    if (evt.shiftKey) {
      this.helper.addToFilter(this.key, this._values);
      prevent = true
    } else if (evt.ctrlKey) {
      this.integration.writeToClipboard(this._values.join(', '))
        .then(() => {
          this.uai.success("Copied to clipboard", "Successfully copied " + this._values.join(", ") + " to your clipboard")
        })
        .catch(err => {
          this.uai.error("Failed to copy to clipboard", this.uai.getErrorMessgae(err))
        })

      prevent = true
    }

    if (prevent) {
      evt.preventDefault();
      evt.stopPropagation();
    }
  }

  @HostBinding('class.border-dashed')
  @HostBinding('class.border-gray-500')
  @HostBinding('class.hover:border-gray-700')
  readonly _styleHost = true;

  @HostBinding('class.cursor-pointer')
  @HostBinding('class.hover:cursor-pointer')
  @HostBinding('class.border-b')
  @HostBinding('class.select-none')
  get shouldHiglight() {
    return this.isShiftKeyPressed || this.isCtrlKeyPressed
  }

  isShiftKeyPressed = false;
  isCtrlKeyPressed = false;

  constructor(
    private helper: NetqueryHelper,
    private uai: ActionIndicatorService,
    private cdr: ChangeDetectorRef,
  ) { }

  ngOnInit(): void {
    this.subscription = combineLatest([this.helper.onShiftKey, this.helper.onCtrlKey])
      .subscribe(([isShiftKeyPressed, isCtrlKeyPressed]) => {
        if (!this.key) {
          return;
        }

        this.isShiftKeyPressed = isShiftKeyPressed;
        this.isCtrlKeyPressed = isCtrlKeyPressed;
        this.cdr.markForCheck();
      })
  }

  ngOnDestroy(): void {
    this.subscription.unsubscribe();
  }
}
