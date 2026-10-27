// WorkBuddy 5.3.8 renderer getModelDisplayName, reconstructed from the installed
// contexts-DgzzFjnf.js. Keep its custom-model overrides when checking host output.
function workBuddyDisplayName(model) {
  const id = model.id.startsWith('custom-local:') ? model.id.slice(13) : model.id;
  const displayID = { 'deepseek-reasoner': 'deepseek-r1', 'deepseek-chat': 'deepseek-v3' }[id] || id;
  return !model.name || model.name === displayID ? displayID : `${model.name}:${displayID}`;
}

module.exports = { workBuddyDisplayName };
