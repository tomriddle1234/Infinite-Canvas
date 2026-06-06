(function () {
  window.WebgenAdapters = window.WebgenAdapters || [];
  if (window.WebgenAdapters.some(adapter => adapter.id === 'generic-chat-image')) return;
  window.WebgenAdapters.push({
    id: 'generic-chat-image',
    label: 'Generic chat image page',
    matches() {
      return /^https?:\/\//i.test(location.href)
        && !/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\//i.test(location.href);
    },
    findInput() {
      const selectors = [
        'rich-textarea .ql-editor',
        'rich-textarea div[contenteditable="true"]',
        '.ql-editor[contenteditable="true"]',
        'div[enterkeyhint="enter"][contenteditable="true"]',
        'textarea:not([readonly])',
        'input[type="text"]:not([readonly])',
        '[contenteditable="true"]'
      ];
      return selectors.map(selector => document.querySelector(selector)).find(isVisible) || document.activeElement;
    },
    promptContainers() {
      return [
        ...document.querySelectorAll('article, [role="article"], [data-message-author-role], main p, main div')
      ];
    },
    searchRoot() {
      return document.querySelector('main') || document.body;
    }
  });

  function isVisible(el) {
    if (!el) return false;
    const rect = el.getBoundingClientRect();
    const style = getComputedStyle(el);
    return rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden';
  }
})();
