package inspect

import (
	"crypto/sha256"
	"encoding/base64"
)

// pickerScript is injected verbatim as the sanitized page's only <script>
// (see Sanitize). It walks up from the click target to the nearest
// data-feedla-id-bearing ancestor and posts that integer to the parent
// window -- nothing else. The parent (SelectorPicker.tsx) is expected to
// validate event.source before trusting the message; this script has no
// way to prove who it's talking to.
//
// It also outlines that same ancestor on hover, purely as in-frame visual
// feedback (no postMessage involved) -- this needs no allow-same-origin
// grant because it only ever touches the iframe's own document, never the
// parent window.
//
// The inbound direction exists too: the parent may post
// {type: 'feedla-inspect-highlight', groups: [[id, ...], ...]} and the
// script frames each group's elements in that group's color, so the user
// can see what each generated selector candidate actually matches. The
// only guard is ev.source === parent -- the frame can't authenticate its
// embedder any further -- which is fine because the payload is non-secret
// integers and the effect is purely cosmetic inside this document.
// HIGHLIGHT_COLORS is index-synced with CANDIDATE_COLORS in
// web/src/components/SelectorPicker.tsx (the candidate-row swatches).
// box-shadow, not outline, so it can't collide with the hover feedback's
// save/restore of el.style.outline above.
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

  var HIGHLIGHT_COLORS = ['#ea580c', '#0891b2', '#9333ea'];
  var highlighted = [];
  var idMap = null;

  function elementsById(){
    if (idMap) return idMap;
    idMap = {};
    var els = document.querySelectorAll('[data-feedla-id]');
    for (var i = 0; i < els.length; i++) {
      idMap[els[i].getAttribute('data-feedla-id')] = els[i];
    }
    return idMap;
  }

  function clearHighlights(){
    // Reverse order: an element framed by two overlapping groups saved its
    // original box-shadow first and the first group's frame second --
    // undoing last-to-first lands back on the original.
    for (var i = highlighted.length - 1; i >= 0; i--) {
      highlighted[i].el.style.boxShadow = highlighted[i].prev;
    }
    highlighted = [];
  }

  window.addEventListener('message', function(ev){
    if (ev.source !== parent) return;
    var d = ev.data;
    if (!d || d.type !== 'feedla-inspect-highlight' || !Array.isArray(d.groups)) return;
    clearHighlights();
    var map = elementsById();
    for (var g = 0; g < d.groups.length && g < HIGHLIGHT_COLORS.length; g++) {
      var ids = d.groups[g];
      if (!Array.isArray(ids)) continue;
      for (var i = 0; i < ids.length; i++) {
        if (typeof ids[i] !== 'number') continue;
        var el = map[String(ids[i])];
        if (!el) continue;
        highlighted.push({el: el, prev: el.style.boxShadow});
        el.style.boxShadow = 'inset 0 0 0 2px ' + HIGHLIGHT_COLORS[g];
      }
    }
  });
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
