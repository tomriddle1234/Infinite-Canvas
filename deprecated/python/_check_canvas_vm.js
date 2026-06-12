const fs = require("fs");
const vm = require("vm");

const html = fs.readFileSync("static/canvas.html", "utf8");
const match = [...html.matchAll(/<script(?![^>]*src=)[^>]*>([\s\S]*?)<\/script>/gi)][0];
const code = match[1];

function makeElement() {
  return new Proxy(function () {}, {
    get(_target, prop) {
      if (prop === Symbol.toPrimitive) return () => "";
      if (prop === "classList") return { add() {}, remove() {}, toggle() {}, contains() { return false; } };
      if (prop === "style" || prop === "dataset") return {};
      if (prop === "children") return [];
      if (prop === "getContext") return () => ({
        clearRect() {}, getImageData() { return { data: new Uint8ClampedArray(4) }; }, putImageData() {},
        beginPath() {}, moveTo() {}, lineTo() {}, stroke() {}, fill() {}, drawImage() {},
        save() {}, restore() {}, strokeRect() {}, ellipse() {}, strokeText() {}, fillText() {},
      });
      if (prop === "querySelectorAll") return () => [];
      if (prop === "querySelector") return () => makeElement();
      if (prop === "addEventListener") return () => {};
      if (["appendChild", "append", "remove", "insertAdjacentHTML", "setAttribute"].includes(prop)) return () => {};
      if (prop === "getBoundingClientRect") return () => ({ left: 0, top: 0, width: 100, height: 100 });
      if (["clientWidth", "clientHeight", "offsetWidth", "offsetHeight", "width", "height"].includes(prop)) return 100;
      return makeElement();
    },
    set() { return true; },
    apply() { return makeElement(); },
  });
}

const context = {
  console,
  document: {
    getElementById() { return makeElement(); },
    querySelector() { return makeElement(); },
    querySelectorAll() { return []; },
    addEventListener() {},
    body: makeElement(),
    documentElement: makeElement(),
    createElement() { return makeElement(); },
  },
  localStorage: { getItem() { return null; }, setItem() {}, removeItem() {} },
  sessionStorage: { getItem() { return null; }, setItem() {} },
  fetch: async () => ({ ok: true, json: async () => ({}), text: async () => "", blob: async () => ({}) }),
  URL: { createObjectURL() { return ""; }, revokeObjectURL() {} },
  Blob: function () {},
  File: function () {},
  FileReader: function () {},
  Image: function () { return makeElement(); },
  FormData: function () { this.append = () => {}; },
  BroadcastChannel: function () { this.postMessage = () => {}; },
  setTimeout,
  clearTimeout,
  setInterval,
  clearInterval,
  requestAnimationFrame: (cb) => setTimeout(cb, 0),
  CSS: { escape: (s) => String(s) },
  navigator: { clipboard: { writeText: async () => {} } },
  lucide: { createIcons() {} },
  performance: { now: Date.now },
  alert: console.log,
  confirm: () => true,
  addEventListener() {},
  removeEventListener() {},
};
context.window = context;
context.location = { search: "" };

vm.createContext(context);
vm.runInContext(code, context, { filename: "canvas.html" });
console.log("VM LOAD OK", typeof context.addPromptNode, typeof context.menuAdd);
vm.runInContext("canvas={id:'x',nodes:[],connections:[],viewport:{x:0,y:0,k:1}}; nodes=canvas.nodes; connections=canvas.connections; addPromptNode(); console.log('nodes', nodes.length, nodes[0] && nodes[0].type);", context);
