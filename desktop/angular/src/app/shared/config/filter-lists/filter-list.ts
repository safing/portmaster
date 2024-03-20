import { ChangeDetectionStrategy, ChangeDetectorRef, Component, forwardRef, HostListener, OnDestroy, OnInit } from '@angular/core';
import { ControlValueAccessor, NG_VALUE_ACCESSOR } from '@angular/forms';
import { PortapiService, Record } from '@safing/portmaster-api';
import { Subscription } from 'rxjs';
import { moveInOutListAnimation } from '../../animations';

interface Category {
  name: string;
  id: string;
  description: string;
  parent?: string | null;
}

interface Source {
  name: string;
  id: string;
  description: string;
  category: string;
  // urls: Resource[]; // we don't care about the actual URLs here.
  website: string;
  contribute: string;
  license: string;
}

interface FilterListIndex extends Record {
  version: string;
  schemaVersion: string;
  categories: Category[];
  sources: Source[];
}

interface TreeNode {
  id: string;
  name: string;
  description: string;
  children: TreeNode[];
  expanded: boolean;
  selected: boolean;
  parent?: TreeNode;
  website?: string;
  license?: string;
  hasSelectedChildren: boolean;
}

@Component({
  selector: 'app-filter-list',
  templateUrl: './filter-list.html',
  styleUrls: ['./filter-list.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
  providers: [
    {
      provide: NG_VALUE_ACCESSOR,
      useExisting: forwardRef(() => FilterListComponent),
      multi: true,
    }
  ],
  animations: [
    moveInOutListAnimation,
  ]
})
export class FilterListComponent implements OnInit, OnDestroy, ControlValueAccessor {
  /** The actual filter-list index as loaded from the portmaster. */
  private index: FilterListIndex | null = null;

  /** @private a list of "tree-nodes" to render */
  nodes: TreeNode[] = [];

  /** A lookup map for fast ID to TreeNode lookups */
  private lookupMap: Map<string, TreeNode> = new Map();

  /** @private forward blur events to the onTouch callback. */
  @HostListener('blur')
  onBlur() {
    this.onTouch();
  }

  /** The currently selected IDs. */
  private selectedIDs: string[] = [];

  /** Subscription to watch the filterlist index. */
  private watchSubscription = Subscription.EMPTY;

  constructor(private portapi: PortapiService,
    private changeDetectorRef: ChangeDetectorRef) { }

  ngOnInit() {
    this.watchSubscription =
      this.portapi.watch<FilterListIndex>("cache:intel/filterlists/index")
        .subscribe(
          index => this.updateIndex(index),
          err => {
            // Filter list index not yet loaded.
            console.error(`failed to get fitlerlist index`, err);
          }
        );
  }

  ngOnDestroy() {
    this.watchSubscription.unsubscribe();
  }

  /** The onChange callback registered by ngModel or form controls */
  private _onChange: (v: string[]) => void = () => { };

  /** Registers the onChange callback required by ControlValueAccessor */
  registerOnChange(fn: (v: string[]) => void) {
    this._onChange = fn;
  }

  /** The _onTouch callback registered by ngModel and form controls */
  private onTouch: () => void = () => { };

  /** Registeres the onTouch callback required by ControlValueAccessor. */
  registerOnTouched(fn: () => void) {
    this.onTouch = fn;
  }

  /**
   * Update the currently selected IDs. Used by ngModel
   * and form controls. Implements ControlValueAccessor.
   *
   * @param ids A list of selected IDs
   */
  writeValue(ids: string[]) {
    this.selectedIDs = ids;
    if (!!this.index) {
      this.updateIndex(this.index);
    }
  }

  /**
   *
   * @param index The filter list index.
   */
  private updateIndex(index: FilterListIndex) {
    this.index = index;

    var nodes: TreeNode[] = [];
    let lm = new Map<string, TreeNode>();
    let childCategories: Category[] = [];

    // Create a tree-node for each category
    this.index.categories.forEach(category => {
      let tn: TreeNode = {
        id: category.id,
        description: category.description,
        name: category.name,
        children: [],
        expanded: this.lookupMap.get(category.id)?.expanded || false, // keep it expanded if the user did not change anything.
        selected: false,
        hasSelectedChildren: false,
      };

      lm.set(category.id, tn)

      // if the category does not have a parent
      // it's a root node.
      if (!category.parent) {
        nodes.push(tn);
      } else {
        // we need to handle child-categories later.
        childCategories.push(category);
      }
    });

    // iterate over all "child" categories and add
    // them to the correct parent (which must be in lm already.)
    childCategories.forEach(category => {
      const tn = lm.get(category.id)!;
      const parent = lm.get(category.parent!);
      // if the parent category does not exist ignore it
      if (!parent) {
        return;
      }

      parent.children.push(tn);
      tn.parent = parent;
    });

    this.index.sources.forEach(source => {
      let category = lm.get(source.category);
      if (!category) {
        return;
      }

      let tn: TreeNode = {
        id: source.id,
        name: source.name,
        description: source.description,
        children: [],
        expanded: false,
        selected: false,
        parent: category,
        website: source.website,
        license: source.license,
        hasSelectedChildren: false
      }

      // Add the source to the lookup-map
      lm.set(source.id, tn);

      category.children.push(tn);
    });

    // make sure we expand all parent categories for
    // all selected IDs so they are actually visible.
    this.selectedIDs.forEach(id => {
      const tn = lm.get(id);
      if (!tn) {
        return;
      }

      this.updateNode(tn, true, true, true, false);

      let parent = tn.parent;
      while (!!parent) {
        parent.expanded = true;
        parent.hasSelectedChildren = true;
        parent = parent.parent;
      }
    });

    this.nodes = nodes;
    this.lookupMap = lm;

    this.changeDetectorRef.markForCheck();
  }

  /** Returns all actually selected IDs. */
  private getIDs() {
    let ids: string[] = [];

    let collectIds = (n: TreeNode) => {
      if (n.selected) {
        // If the parent is selected we can ignore the
        // childs because they must be selected as well.
        ids.push(n.id);
        return;
      }

      n.children.forEach(child => collectIds(child));
    }

    this.nodes.forEach(node => collectIds(node))

    return ids;
  }

  updateNode(node: TreeNode, selected: boolean, updateChildren = true, updateParents = true, emit = true) {
    if (node.selected === selected) {
      // Nothing changed
      return;
    }

    // update the node an all children
    node.selected = selected;
    if (updateChildren) {
      node.children.forEach(child => this.updateNode(child, selected, true, false, false));
    }

    // if we have a parent we might need to update
    // the parent as well.
    if (!!node.parent && updateParents) {
      if (selected) {
        // if we are now selected we might need to "select" the
        // parent if all children are selected now.
        const hasUnselected = node.parent.children.some(sibling => !sibling.selected);
        if (!hasUnselected) {
          // We need to update all parents but updating children
          // is useless.
          this.updateNode(node.parent, true, false, true, false);
        }
      } else if (node.parent.selected) {
        // if we are unselected now we might need to "unselect" the parent
        // but select siblings directly
        const selectedSiblings = node.parent.children.filter(sibling => sibling.selected && sibling !== node);
        this.updateNode(node.parent, false, false, true, false)
      }
    }

    if (emit) {
      const ids = this.getIDs();
      this.selectedIDs = ids;
      this._onChange(this.selectedIDs);
    }
  }

  /** @private TrackByFunction for tree nodes. */
  trackNode(_: number, node: TreeNode) {
    return node.id;
  }
}

