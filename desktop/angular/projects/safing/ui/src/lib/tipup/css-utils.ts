
export function synchronizeCssStyles(src: HTMLElement, destination: HTMLElement, skipStyles: Set<string>) {
  // Get a list of all the source and destination elements
  const srcElements = <HTMLCollectionOf<HTMLElement>>src.getElementsByTagName('*');
  const dstElements = <HTMLCollectionOf<HTMLElement>>destination.getElementsByTagName('*');

  cloneStyle(src, destination, skipStyles);

  // For each element
  for (let i = srcElements.length; i--;) {
    const srcElement = srcElements[i];
    const dstElement = dstElements[i];
    cloneStyle(srcElement, dstElement, skipStyles);
  }
}

function cloneStyle(srcElement: HTMLElement, dstElement: HTMLElement, skipStyles: Set<string>) {
  const sourceElementStyles = document.defaultView!.getComputedStyle(srcElement, '');
  const styleAttributeKeyNumbers = Object.keys(sourceElementStyles);

  // Copy the attribute
  for (let j = 0; j < styleAttributeKeyNumbers.length; j++) {
    const attributeKeyNumber = styleAttributeKeyNumbers[j];
    const attributeKey: string = sourceElementStyles[attributeKeyNumber as any];
    if (!isNaN(+attributeKey)) {
      continue
    }
    if (attributeKey === 'cssText') {
      continue
    }

    if (skipStyles.has(attributeKey)) {
      continue
    }

    try {
      dstElement.style[attributeKey as any] = sourceElementStyles[attributeKey as any];
    } catch (e) {
      console.error(attributeKey, e);
    }
  }
}

/**
 * Returns a CSS selector for el from rootNode.
 *
 * @param el The source element to get the CSS path to
 * @param rootNode The root node at which the CSS path should be applyable
 * @returns A CSS selector to access el from rootNode.
 */
export function getCssSelector(el: HTMLElement, rootNode: HTMLElement | null): string {
  if (!el) {
    return '';
  }
  let stack = [];
  let isShadow = false;
  while (el !== rootNode && el.parentNode !== null) {
    // console.log(el.nodeName);
    let sibCount = 0;
    let sibIndex = 0;
    // get sibling indexes
    for (let i = 0; i < (el.parentNode as HTMLElement).childNodes.length; i++) {
      let sib = (el.parentNode as HTMLElement).childNodes[i];
      if (sib.nodeName == el.nodeName) {
        if (sib === el) {
          sibIndex = sibCount;
        }
        sibCount++;
      }
    }
    let nodeName = el.nodeName.toLowerCase();
    if (isShadow) {
      throw new Error(`cannot traverse into shadow dom.`)
    }
    if (sibCount > 1) {
      stack.unshift(nodeName + ':nth-of-type(' + (sibIndex + 1) + ')');
    } else {
      stack.unshift(nodeName);
    }
    el = el.parentNode as HTMLElement;
    if (el.nodeType === 11) { // for shadow dom, we
      isShadow = true;
      el = (el as any).host;
    }
  }
  return stack.join(' > ');
}
