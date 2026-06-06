(function () {
  window.WebgenAdapters = window.WebgenAdapters || [];
  if (window.WebgenAdapters.some(adapter => adapter.id === 'chatgpt')) return;
  window.WebgenAdapters.push({
    id: 'chatgpt',
    label: 'ChatGPT',
    matches() {
      return /^https:\/\/chatgpt\.com\//i.test(location.href) || /^https:\/\/chat\.openai\.com\//i.test(location.href);
    },
    findInput() {
      return document.querySelector('#prompt-textarea')
        || document.querySelector('[data-testid="prompt-textarea"]')
        || document.querySelector('textarea[data-id="root"]')
        || document.querySelector('div[contenteditable="true"]');
    },
    promptContainers() {
      return [
        ...document.querySelectorAll('[data-message-author-role="user"], article, [role="article"]')
      ];
    },
    searchRoot() {
      return document.querySelector('main') || document.body;
    }
  });
})();
