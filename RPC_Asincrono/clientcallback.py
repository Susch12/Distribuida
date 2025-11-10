import threading
import xmlrpc.client
from xmlrpc.server import SimpleXMLRPCServer

# Servidor de callback del cliente
def client_callback(response):
    print(f"Respuesta del servidor: {response}")

##IP LOCAL
callback_server = SimpleXMLRPCServer(("172.31.12.109", 9000), allow_none=True, logRequests=False)
callback_server.register_function(client_callback, "client_callback")

# Hilo para correr el servidor de callback del cliente
def run_callback_server():
    print("Servidor de callback del cliente en el puerto 9000...")
    callback_server.serve_forever()

threading.Thread(target=run_callback_server, daemon=True).start()

# Cliente RPC
server = xmlrpc.client.ServerProxy("http://172.31.5.48:8000/", allow_none=True) ## IP del servidor

def send_request():
    """Función que envía la solicitud al servidor en un hilo separado."""
    print("Solicitud enviada al servidor, esperando respuesta...")
    try:
        server.long_task("http://172.31.12.109:9000/")  # Llamada RPC con callback mandar IP LOCAL
    except Exception as e:
        print(f"Error al conectar con el servidor: {e}")

# Enviar solicitud en un hilo separado
rpc_thread = threading.Thread(target=send_request)
rpc_thread.start()

print("El cliente sigue ejecutándose mientras espera la respuesta...")



