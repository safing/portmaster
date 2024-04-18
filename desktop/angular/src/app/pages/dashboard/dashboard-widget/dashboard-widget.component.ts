import { coerceBooleanProperty } from "@angular/cdk/coercion";
import { ChangeDetectionStrategy, Component, Input } from "@angular/core";

@Component({
  selector: 'app-dashboard-widget',
  templateUrl: './dashboard-widget.component.html',
  styles: [
    `
      :host {
        @apply bg-gray-200 p-4 self-stretch rounded-md flex flex-col gap-2;
      }

      label {
        @apply text-xs uppercase text-secondary font-light flex flex-row items-center gap-2 pb-2;
      }
    `
  ],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class DashboardWidgetComponent {
  @Input()
  set beta(v: any) {
    this._beta = coerceBooleanProperty(v)
  }
  get beta() { return this._beta }
  private _beta = false;

  @Input()
  label: string = '';
}
