import smpp from 'k6/x/smpp';
import { sleep } from 'k6';

export let options = {
  vus: 5,
  iterations: 50,
};

export default function () {
  const session = smpp.connect({
    host: 'smpp.server.com',
    port: 2775,
    system_id: 'test',
    password: 'secret',
    bind: 'transceiver',
  });

  for (let i = 0; i < 5; i++) {
    try {
      const id = session.sendSMS('TEST', '+491701234567', `Hello SMPP ${i}`);
      console.log(`Sent message ${id}`);
    } catch (err) {
      console.error(`Send error: ${err}`);
    }
    sleep(0.5);
  }

  session.close();
}
