package inspect

import (
	"crypto/sha256"
	"encoding/base64"
)

// pickerScript is injected verbatim as the sanitized page's only <script>
// (see Sanitize). It walks up from the click target to the nearest
// data-feedla-id-bearing ancestor and posts that integer to the parent
// window -- nothing else. The parent (Phase F2's frontend, not built in
// this PR) is expected to validate event.source before trusting the
// message; this script has no way to prove who it's talking to.
//
// It also outlines that same ancestor on hover, purely as in-frame visual
// feedback (no postMessage involved) -- this needs no allow-same-origin
// grant because it only ever touches the iframe's own document, never the
// parent window.
//
// The targetOrigin is deliberately "*" rather than the embedding app's
// origin: the payload is a single non-secret integer, and keeping this
// string free of any environment-specific value (public origin, etc.) is
// what lets pickerScriptSHA256 below be computed once, at package init,
// from a byte-for-byte-stable constant instead of being templated per
// request.
const pickerScript = `(function(){
  function pickable(el){
    while (el && !(el.getAttribute && el.getAttribute('data-feedla-id'))) {
      el = el.parentElement;
    }
    return el;
  }

  var hovered = null;
  var hoveredOutline = '';
  var hoveredOutlineOffset = '';

  function clearHover(){
    if (!hovered) return;
    hovered.style.outline = hoveredOutline;
    hovered.style.outlineOffset = hoveredOutlineOffset;
    hovered = null;
  }

  document.addEventListener('mouseover', function(ev){
    var el = pickable(ev.target);
    if (el === hovered) return;
    clearHover();
    if (!el) return;
    hoveredOutline = el.style.outline;
    hoveredOutlineOffset = el.style.outlineOffset;
    el.style.outline = '2px solid #2563eb';
    el.style.outlineOffset = '-2px';
    hovered = el;
  }, true);

  document.addEventListener('mouseleave', clearHover, true);

  document.addEventListener('click', function(ev){
    var el = pickable(ev.target);
    if (!el) return;
    ev.preventDefault();
    parent.postMessage(
      {type: 'feedla-inspect-click', id: parseInt(el.getAttribute('data-feedla-id'), 10)},
      '*'
    );
  }, true);
})();`

// PickerScriptSHA256 is the CSP script-src hash-source for the picker
// script embedded by Sanitize -- callers set
// `script-src 'sha256-<PickerScriptSHA256>'` on the view response. It's
// computed from the constant above rather than hardcoded so the header and
// the served bytes can never drift apart.
var PickerScriptSHA256 = computeScriptHash(pickerScript)

func computeScriptHash(script string) string {
	sum := sha256.Sum256([]byte(script))
	return base64.StdEncoding.EncodeToString(sum[:])
}
