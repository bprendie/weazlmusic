import {presets} from './presets.js';
export const albums = [
  {title:'Dive',artist:'Tycho',year:2011,art:0,label:'TYCHO',tracks:[['A Walk','5:16'],['Hours','5:44'],['Daydream','5:34']]},
  {title:'Music Has the Right to Children',artist:'Boards of Canada',year:1998,art:1,label:'BOARDS OF CANADA',tracks:[['Roygbiv','2:31'],['An Eagle in Your Mind','6:23'],['Turquoise Hexagon Sun','5:07']]},
  {title:'In Rainbows',artist:'Radiohead',year:2007,art:2,label:'RADIOHEAD',tracks:[['Weird Fishes / Arpeggi','5:18'],['Nude','4:15'],['Reckoner','4:50']]},
  {title:'Mezzanine',artist:'Massive Attack',year:1998,art:3,label:'MASSIVE ATTACK',tracks:[['Teardrop','5:30'],['Angel','6:18'],['Inertia Creeps','5:56']]}
];
export const tracks = albums.flatMap((album, a) => album.tracks.map(([title,duration], t) => ({id:`${a}-${t}`,title,duration,artist:album.artist,album:album.title,art:a})));
export const stations = [...presets,
  {id:'radio-1',title:'Drone Zone',artist:'SomaFM',genre:'Atmospheric · Ambient · directory sample',duration:'LIVE',art:2,preset:false},
  {id:'radio-3',title:'Indie Pop Rocks',artist:'SomaFM',genre:'Indie · Alternative · directory sample',duration:'LIVE',art:3,preset:false}
];
export const state = {view:'home',query:'',queue:[tracks[1],tracks[3],tracks[6],tracks[9]],current:tracks[0],playing:false,favorites:new Set(),radioTab:'Presets',album:0,user:'bob'};
export const escapeHTML = value => String(value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
