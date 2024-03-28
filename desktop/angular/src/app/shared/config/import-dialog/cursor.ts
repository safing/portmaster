// Credit to Liam (Stack Overflow)
// https://stackoverflow.com/a/41034697/3480193
export class Cursor {
  static getCurrentCursorPosition(parentElement: Node) {
    var selection = window.getSelection(),
      charCount = -1,
      node;

    if (selection?.focusNode) {
      if (Cursor._isChildOf(selection.focusNode, parentElement)) {
        node = selection.focusNode;
        charCount = selection.focusOffset;

        while (node) {
          if (node === parentElement) {
            break;
          }

          if (node.previousSibling) {
            node = node.previousSibling;
            charCount += node.textContent?.length || 0
          } else {
            node = node.parentNode;
            if (node === null) {
              break;
            }
          }
        }
      }
    }

    return charCount;
  }

  static setCurrentCursorPosition(chars: number, element: Node) {
    if (chars >= 0) {
      var selection = window.getSelection();

      let range = Cursor._createRange(element, { count: chars });

      if (range) {
        range.collapse(false);
        selection?.removeAllRanges();
        selection?.addRange(range);
      }
    }
  }

  static _createRange(node: Node, chars: { count: number }, range?: Range): Range {
    if (!range) {
      range = document.createRange()
      range.selectNode(node);
      range.setStart(node, 0);
    }

    if (chars.count === 0) {
      range.setEnd(node, chars.count);
    } else if (node && chars.count > 0) {
      if (node.nodeType === Node.TEXT_NODE) {
        if (node.textContent!.length < chars.count) {
          chars.count -= node.textContent!.length;
        } else {
          range.setEnd(node, chars.count);
          chars.count = 0;
        }
      } else {
        for (var lp = 0; lp < node.childNodes.length; lp++) {
          range = Cursor._createRange(node.childNodes[lp], chars, range);

          if (chars.count === 0) {
            break;
          }
        }
      }
    }

    return range;
  }

  static _isChildOf(node: Node, parentElement: Node) {
    while (node !== null) {
      if (node === parentElement) {
        return true;
      }
      node = node.parentNode!;
    }

    return false;
  }
}
