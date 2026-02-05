import { WebSocket } from "ws";

export interface EchoService {
  name: string;
  ws: WebSocket;
  connectedAt: Date;
}

class Registry {
  private services: Map<string, EchoService> = new Map();

  register(name: string, ws: WebSocket): void {
    // Close existing connection if any
    const existing = this.services.get(name);
    if (existing) {
      existing.ws.close();
    }

    this.services.set(name, {
      name,
      ws,
      connectedAt: new Date(),
    });
    console.log(`Echo service registered: ${name}`);
  }

  unregister(ws: WebSocket): void {
    for (const [name, service] of this.services) {
      if (service.ws === ws) {
        this.services.delete(name);
        console.log(`Echo service unregistered: ${name}`);
        return;
      }
    }
  }

  get(name: string): EchoService | undefined {
    return this.services.get(name);
  }

  list(): { name: string; connectedAt: Date }[] {
    return Array.from(this.services.values()).map((s) => ({
      name: s.name,
      connectedAt: s.connectedAt,
    }));
  }

  sendText(name: string, text: string): boolean {
    const service = this.services.get(name);
    if (!service || service.ws.readyState !== WebSocket.OPEN) {
      return false;
    }

    service.ws.send(JSON.stringify({ type: "text", content: text }));
    return true;
  }
}

export const registry = new Registry();
