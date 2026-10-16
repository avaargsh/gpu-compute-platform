'use client';

import { useEffect, useState, useCallback } from 'react';
import { io, type Socket } from 'socket.io-client';

export function useWebSocket(url: string) {
  const [socket, setSocket] = useState<Socket | null>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const s = io(url, {
      transports: ['websocket'],
      autoConnect: true,
      reconnection: true,
      reconnectionAttempts: 5,
      reconnectionDelay: 1000,
    });
    setSocket(s);

    s.on('connect', () => setConnected(true));
    s.on('disconnect', () => setConnected(false));

    return () => {
      s.disconnect();
      setSocket(null);
      setConnected(false);
    };
  }, [url]);

  const send = useCallback(
    (event: string, data: any) => {
      socket?.emit(event, data);
    },
    [socket]
  );

  return { socket, connected, send };
}
