import PocketBase from './pocketbase.es.mjs';

const pb = new PocketBase(window.location.origin);
const status = document.getElementById('api-status');
try {
  const response = await fetch('/studio/session', {method: 'POST', credentials: 'same-origin'});
  if (!response.ok) throw new Error('Session PocketBase indisponible.');
  const {token, record} = await response.json();
  pb.authStore.save(token, record);
  await pb.collection(record.collectionName).getOne(record.id);
  status.textContent = 'API PocketBase : accès confirmé avec le SDK officiel.';
} catch {
  pb.authStore.clear();
  status.textContent = "L'accès à l'API PocketBase n'a pas pu être confirmé.";
}

document.querySelector('form[action="/studio/logout"]').addEventListener('submit', () => {
  pb.authStore.clear();
  pb.realtime.unsubscribe();
});
