(function() {
    // GoFastr Core-UI Runtime v0.1
    'use strict';
    
    var handlers = {};
    
    window.__gofastr = {
        register: function(id, events) {
            handlers[id] = events;
        },
        trigger: function(id, event, params) {
            if (handlers[id] && handlers[id][event]) {
                handlers[id][event](params);
            }
        },
        handlers: handlers
    };
    
    // Event delegation for clicks
    document.addEventListener('click', function(e) {
        var target = e.target.closest('[data-action]');
        if (!target) return;
        var action = target.getAttribute('data-action');
        var componentId = closestAttr(e.target, 'data-component');
        if (componentId && action) {
            e.preventDefault();
            window.__gofastr.trigger(componentId, action, collectParams(target));
        }
    });
    
    // Event delegation for input/change/submit
    ['input', 'change', 'submit'].forEach(function(eventType) {
        document.addEventListener(eventType, function(e) {
            var attrName = 'data-action-' + eventType;
            var target = e.target.closest('[' + attrName + ']');
            if (!target) return;
            var action = target.getAttribute(attrName);
            var componentId = closestAttr(e.target, 'data-component');
            if (componentId && action) {
                e.preventDefault();
                window.__gofastr.trigger(componentId, action, collectParams(target));
            }
        });
    });
    
    function closestAttr(el, attr) {
        var node = el;
        while (node && node !== document) {
            if (node.getAttribute && node.getAttribute(attr)) {
                return node.getAttribute(attr);
            }
            node = node.parentNode;
        }
        return null;
    }
    
    function collectParams(el) {
        var params = {};
        if (!el || !el.attributes) return params;
        for (var i = 0; i < el.attributes.length; i++) {
            var attr = el.attributes[i];
            if (attr.name.indexOf('data-param-') === 0) {
                params[attr.name.slice('data-param-'.length)] = attr.value;
            }
        }
        return params;
    }
    
    // Hydration on first interaction
    var hydrated = {};
    
    function hydrate(componentId) {
        if (hydrated[componentId]) return;
        hydrated[componentId] = true;
        
        var el = document.querySelector('[data-component="' + componentId + '"]');
        if (!el) return;
        
        var scriptSrc = el.getAttribute('data-behavior');
        if (scriptSrc) {
            var script = document.createElement('script');
            script.src = scriptSrc;
            document.head.appendChild(script);
        }
    }
    
    // Observe DOM for new components
    if (typeof MutationObserver !== 'undefined') {
        var observer = new MutationObserver(function(mutations) {
            for (var i = 0; i < mutations.length; i++) {
                var added = mutations[i].addedNodes;
                for (var j = 0; j < added.length; j++) {
                    observeNode(added[j]);
                }
            }
        });
        
        function observeNode(node) {
            if (node.nodeType !== 1) return;
            if (node.getAttribute && node.getAttribute('data-component')) {
                setupHydration(node);
            }
            var children = node.querySelectorAll ? node.querySelectorAll('[data-component]') : [];
            for (var i = 0; i < children.length; i++) {
                setupHydration(children[i]);
            }
        }
        
        function setupHydration(el) {
            el.addEventListener('focus', function() { hydrate(el.getAttribute('data-component')); }, { once: true });
            el.addEventListener('mouseenter', function() { hydrate(el.getAttribute('data-component')); }, { once: true });
        }
        
        if (document.body) {
            observer.observe(document.body, { childList: true, subtree: true });
        }
    }
    
    // SSE Island Support
    function connectSSE() {
        var metaTag = document.querySelector('meta[name="gofastr-sse"]');
        if (!metaTag) return;
        var sseUrl = metaTag.getAttribute('content');
        if (!sseUrl) return;
        
        var source = new EventSource(sseUrl);
        
        source.addEventListener('island', function(event) {
            try {
                var data = JSON.parse(event.data);
                if (data.island && data.html !== undefined) {
                    var el = document.querySelector('[data-island="' + data.island + '"]');
                    if (el) {
                        el.innerHTML = data.html;
                    }
                }
            } catch(e) {}
        });
        
        source.onerror = function() {
            source.close();
            setTimeout(connectSSE, 3000);
        };
    }
    
    // Connect SSE when DOM ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', connectSSE);
    } else {
        connectSSE();
    }
})();
