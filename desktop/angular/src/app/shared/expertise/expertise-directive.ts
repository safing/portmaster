import { Directive, EmbeddedViewRef, Input, isDevMode, OnDestroy, OnInit, TemplateRef, ViewContainerRef } from '@angular/core';
import { ExpertiseLevelNumber } from '@safing/portmaster-api';
import { Subscription } from 'rxjs';
import { ExpertiseService } from './expertise.service';

// ExpertiseLevelOverwrite may be called to display a DOM node decorated
// with [appExpertiseLevel] even if the current user setting does not
// match the required expertise.
export type ExpertiseLevelOverwrite<T> = (lvl: ExpertiseLevelNumber, data: T) => boolean;
@Directive({
  selector: '[appExpertiseLevel]',
})
export class ExpertiseDirective<T> implements OnInit, OnDestroy {
  private allowedValue: ExpertiseLevelNumber = ExpertiseLevelNumber.user;
  private subscription = Subscription.EMPTY;
  private view: EmbeddedViewRef<any> | null = null;

  @Input()
  set appExpertiseLevelOverwrite(fn: ExpertiseLevelOverwrite<T>) {
    this._levelOverwriteFn = fn;
    this.update();
  }
  private _levelOverwriteFn: ExpertiseLevelOverwrite<T> | null = null;

  @Input()
  set appExpertiseLevelData(d: T) {
    this._data = d;
    this.update();
  }
  private _data: T | undefined = undefined;

  @Input()
  set appExpertiseLevel(lvl: ExpertiseLevelNumber | string) {
    if (typeof lvl === 'string') {
      lvl = ExpertiseLevelNumber[lvl as any];
    }
    if (lvl === undefined) {
      if (isDevMode()) {
        throw new Error(`[appExpertiseLevel] got undefined expertise-level value`);
      }
      return;
    }
    if (lvl !== this.allowedValue) {
      this.allowedValue = lvl as ExpertiseLevelNumber;
      this.update();
    }
  }

  private update() {
    const current = ExpertiseLevelNumber[this.expertiseService.currentLevel];
    let hide = current < this.allowedValue;

    // if there's an overwrite function defined make sue to check that.
    if (hide && !!this._levelOverwriteFn) {
      hide = !this._levelOverwriteFn(current, this._data!);
      if (!hide) {
        console.log("overwritten", current, this._data);
      }
    }

    if (hide) {
      if (!!this.view) {
        this.view.destroy();
        this.viewContainer.clear();
        this.view = null;
      }
      return
    }

    if (!!this.view) {
      this.view.markForCheck();
      return;
    }

    this.view = this.viewContainer.createEmbeddedView(this.templateRef);
    this.view.detectChanges();
  }

  constructor(
    private expertiseService: ExpertiseService,
    private templateRef: TemplateRef<any>,
    private viewContainer: ViewContainerRef
  ) { }

  ngOnInit() {
    this.subscription = this.expertiseService.change.subscribe(() => this.update())
  }

  ngOnDestroy() {
    this.viewContainer.clear();
    this.subscription.unsubscribe();
  }
}
